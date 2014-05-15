package router

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"time"

	vcap "github.com/cloudfoundry/gorouter/common"
	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/log"
	"github.com/cloudfoundry/gorouter/proxy"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/util"
	"github.com/cloudfoundry/gorouter/varz"
	"github.com/cloudfoundry/yagnats"

	"github.com/cloudfoundry/gorouter/access_log"
)

type Router struct {
	config     *config.Config
	proxy      proxy.Proxy
	mbusClient *yagnats.Client
	registry   *registry.CFRegistry
	varz       varz.Varz
	component  *vcap.VcapComponent
}

func NewRouter(cfg *config.Config, mbusClient *yagnats.Client, r *registry.CFRegistry, v varz.Varz) (*Router, error) {
	router := &Router{
		config:     cfg,
		mbusClient: mbusClient,
		registry:   r,
		varz:       v,
	}

	// setup number of procs
	if cfg.GoMaxProcs != 0 {
		runtime.GOMAXPROCS(cfg.GoMaxProcs)
	}

	router.registry.StartPruningCycle()

	accesslog, err := access_log.CreateRunningAccessLogger(cfg)
	if err != nil {
		return nil, err
	}

	args := proxy.ProxyArgs{
		EndpointTimeout: cfg.EndpointTimeout,
		Ip:              cfg.Ip,
		TraceKey:        cfg.TraceKey,
		Registry:        router.registry,
		Reporter:        router.varz,
		AccessLogger:    accesslog,
	}
	router.proxy = proxy.NewProxy(args)

	var host string
	if cfg.Status.Port != 0 {
		host = fmt.Sprintf("%s:%d", cfg.Ip, cfg.Status.Port)
	}

	varz := &vcap.Varz{
		UniqueVarz: router.varz,
	}
	varz.LogCounts = log.Counter

	healthz := &vcap.Healthz{
		LockableObject: router.registry,
	}

	router.component = &vcap.VcapComponent{
		Type:        "Router",
		Index:       router.config.Index,
		Host:        host,
		Credentials: []string{cfg.Status.User, cfg.Status.Pass},
		Config:      cfg,
		Varz:        varz,
		Healthz:     healthz,
		InfoRoutes: map[string]json.Marshaler{
			"/routes": router.registry,
		},
	}

	err = vcap.StartComponent(router.component)
	if err != nil {
		return nil, err
	}

	return router, nil
}

func (r *Router) Run() <-chan error {
	var err error

	util.WritePidFile(r.config.Pidfile)

	natsMembers := []yagnats.ConnectionProvider{}

	for _, info := range r.config.Nats {
		natsMembers = append(natsMembers, &yagnats.ConnectionInfo{
			Addr:     fmt.Sprintf("%s:%d", info.Host, info.Port),
			Username: info.User,
			Password: info.Pass,
		})
	}

	natsInfo := &yagnats.ConnectionCluster{natsMembers}

	for {
		err = r.mbusClient.Connect(natsInfo)
		if err == nil {
			log.Infof("Connected to NATS")
			break
		}

		log.Errorf("Could not connect to NATS: %s", err)
		time.Sleep(500 * time.Millisecond)
	}

	r.RegisterComponent()

	// Subscribe register/unregister router
	r.SubscribeRegister()
	r.HandleGreetings()
	r.SubscribeUnregister()

	// Kickstart sending start messages
	r.SendStartMessage()

	// Send start again on reconnect
	r.mbusClient.ConnectedCallback = func() {
		r.SendStartMessage()
	}

	// Schedule flushing active app's app_id
	r.ScheduleFlushApps()

	// Wait for one start message send interval, such that the router's registry
	// can be populated before serving requests.
	if r.config.StartResponseDelayInterval != 0 {
		log.Infof("Waiting %s before listening...", r.config.StartResponseDelayInterval)
		time.Sleep(r.config.StartResponseDelayInterval)
	}

	listen, err := net.Listen("tcp", fmt.Sprintf(":%d", r.config.Port))
	if err != nil {
		log.Fatalf("net.Listen: %s", err)
	}

	log.Infof("Listening on %s", listen.Addr())

	server := http.Server{Handler: r.proxy}

	errChan := make(chan error, 1)
	go func() {
		err := server.Serve(listen)
		errChan <- err
	}()

	return errChan
}

func (r *Router) RegisterComponent() {
	vcap.Register(r.component, r.mbusClient)
}

func (r *Router) SubscribeRegister() {
	r.subscribeRegistry("router.register", func(registryMessage *registryMessage) {
		log.Debugf("Got router.register: %v", registryMessage)

		for _, uri := range registryMessage.Uris {
			r.registry.Register(
				uri,
				registryMessage.makeEndpoint(),
			)
		}
	})
}

func (r *Router) SubscribeUnregister() {
	r.subscribeRegistry("router.unregister", func(registryMessage *registryMessage) {
		log.Debugf("Got router.unregister: %v", registryMessage)

		for _, uri := range registryMessage.Uris {
			r.registry.Unregister(
				uri,
				registryMessage.makeEndpoint(),
			)
		}
	})
}

func (r *Router) HandleGreetings() {
	r.mbusClient.Subscribe("router.greet", func(msg *yagnats.Message) {
		response, _ := r.greetMessage()
		r.mbusClient.Publish(msg.ReplyTo, response)
	})
}

func (r *Router) SendStartMessage() {
	b, err := r.greetMessage()
	if err != nil {
		panic(err)
	}

	// Send start message once at start
	r.mbusClient.Publish("router.start", b)
}

func (r *Router) ScheduleFlushApps() {
	if r.config.PublishActiveAppsInterval == 0 {
		return
	}

	go func() {
		t := time.NewTicker(r.config.PublishActiveAppsInterval)
		x := time.Now()

		for {
			select {
			case <-t.C:
				y := time.Now()
				r.flushApps(x)
				x = y
			}
		}
	}()
}

func (r *Router) flushApps(t time.Time) {
	x := r.varz.ActiveApps().ActiveSince(t)

	y, err := json.Marshal(x)
	if err != nil {
		log.Warnf("flushApps: Error marshalling JSON: %s", err)
		return
	}

	b := bytes.Buffer{}
	w := zlib.NewWriter(&b)
	w.Write(y)
	w.Close()

	z := b.Bytes()

	log.Debugf("Active apps: %d, message size: %d", len(x), len(z))

	r.mbusClient.Publish("router.active_apps", z)
}

func (r *Router) greetMessage() ([]byte, error) {
	host, err := vcap.LocalIP()
	if err != nil {
		return nil, err
	}

	uuid, err := vcap.GenerateUUID()
	if err != nil {
		return nil, err
	}

	d := vcap.RouterStart{
		uuid,
		[]string{host},
		r.config.StartResponseDelayIntervalInSeconds,
	}

	return json.Marshal(d)
}

func (r *Router) subscribeRegistry(subject string, successCallback func(*registryMessage)) {
	callback := func(message *yagnats.Message) {
		payload := message.Payload

		var msg registryMessage

		err := json.Unmarshal(payload, &msg)
		if err != nil {
			logMessage := fmt.Sprintf("%s: Error unmarshalling JSON (%d; %s): %s", subject, len(payload), payload, err)
			log.Warnd(map[string]interface{}{"payload": string(payload)}, logMessage)
		}

		logMessage := fmt.Sprintf("%s: Received message", subject)
		log.Debugd(map[string]interface{}{"message": msg}, logMessage)

		successCallback(&msg)
	}

	_, err := r.mbusClient.Subscribe(subject, callback)
	if err != nil {
		log.Errorf("Error subscribing to %s: %s", subject, err)
	}
}
