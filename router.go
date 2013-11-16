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
	"github.com/cloudfoundry/gorouter/route"
	"github.com/cloudfoundry/gorouter/util"
	"github.com/cloudfoundry/gorouter/varz"
	"github.com/cloudfoundry/yagnats"
)

type Router struct {
	config     *config.Config
	proxy      *proxy.Proxy
	mbusClient *yagnats.Client
	registry   *registry.Registry
	varz       varz.Varz
	component  *vcap.VcapComponent
}

func NewRouter(c *config.Config) *Router {
	router := &Router{
		config: c,
	}

	// setup number of procs
	if router.config.GoMaxProcs != 0 {
		runtime.GOMAXPROCS(router.config.GoMaxProcs)
	}

	router.mbusClient = yagnats.NewClient()

	router.registry = registry.NewRegistry(router.config, router.mbusClient)
	router.registry.StartPruningCycle()

	router.varz = varz.NewVarz(router.registry)
	router.proxy = proxy.NewProxy(router.config, router.registry, router.varz)

	var host string
	if router.config.Status.Port != 0 {
		host = fmt.Sprintf("%s:%d", router.config.Ip, router.config.Status.Port)
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
		Credentials: []string{router.config.Status.User, router.config.Status.Pass},
		Config:      router.config,
		Varz:        varz,
		Healthz:     healthz,
		InfoRoutes: map[string]json.Marshaler{
			"/routes": router.registry,
		},
	}

	vcap.StartComponent(router.component)

	return router
}

func (r *Router) Run() {
	var err error

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

	util.WritePidFile(r.config.Pidfile)

	log.Infof("Listening on %s", listen.Addr())

	server := http.Server{Handler: r.proxy}

	go func() {
		err := server.Serve(listen)
		if err != nil {
			log.Fatalf("proxy.Serve: %s", err)
		}
	}()
}

func (r *Router) RegisterComponent() {
	vcap.Register(r.component, r.mbusClient)
}

type registryMessage struct {
	Host string            `json:"host"`
	Port uint16            `json:"port"`
	Uris []route.Uri       `json:"uris"`
	Tags map[string]string `json:"tags"`
	App  string            `json:"app"`

	PrivateInstanceId string `json:"private_instance_id"`
}

func (r *Router) SubscribeRegister() {
	r.subscribeRegistry("router.register", func(registryMessage *registryMessage) {
		log.Infof("Got router.register: %v", registryMessage)

		for _, uri := range registryMessage.Uris {
			r.registry.Register(
				uri,
				makeRouteEndpoint(registryMessage),
			)
		}
	})
}

func (r *Router) SubscribeUnregister() {
	r.subscribeRegistry("router.unregister", func(registryMessage *registryMessage) {
		log.Infof("Got router.unregister: %v", registryMessage)

		for _, uri := range registryMessage.Uris {
			r.registry.Unregister(
				uri,
				makeRouteEndpoint(registryMessage),
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
	x := r.registry.ActiveSince(t)

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

	d := vcap.RouterStart{
		vcap.GenerateUUID(),
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

func makeRouteEndpoint(registryMessage *registryMessage) *route.Endpoint {
	return &route.Endpoint{
		Host: registryMessage.Host,
		Port: registryMessage.Port,

		ApplicationId: registryMessage.App,
		Tags:          registryMessage.Tags,

		PrivateInstanceId: registryMessage.PrivateInstanceId,
	}
}
