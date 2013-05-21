package router

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	mbus "github.com/cloudfoundry/go_cfmessagebus"
	steno "github.com/cloudfoundry/gosteno"
	"net"
	vcap "router/common"
	"router/config"
	"router/proxy"
	"router/util"
	"runtime"
	"time"
)

type Router struct {
	config     *config.Config
	proxy      *Proxy
	mbusClient mbus.CFMessageBus
	registry   *Registry
	varz       Varz
	component  *vcap.VcapComponent
}

func NewRouter(c *config.Config) *Router {
	r := &Router{
		config: c,
	}

	// setup number of procs
	if r.config.GoMaxProcs != 0 {
		runtime.GOMAXPROCS(r.config.GoMaxProcs)
	}

	r.establishMBus()

	r.registry = NewRegistry(r.config)
	r.registry.isStateStale = func() bool {
		return !r.mbusClient.Ping()
	}

	r.varz = NewVarz(r.registry)
	r.proxy = NewProxy(r.config, r.registry, r.varz)

	var host string
	if r.config.Status.Port != 0 {
		host = fmt.Sprintf("%s:%d", r.config.Ip, r.config.Status.Port)
	}

	varz := &vcap.Varz{
		UniqueVarz: r.varz,
	}
	
	healthz := &vcap.Healthz{
		LockableObject: r.registry,
	}

	r.component = &vcap.VcapComponent{
		Type:        "Router",
		Index:       r.config.Index,
		Host:        host,
		Credentials: []string{r.config.Status.User, r.config.Status.Pass},
		Config:      r.config,
		Logger:      log,
		Varz:        varz,
		Healthz:     healthz,
		InfoRoutes: map[string]json.Marshaler{
			"/routes": r.registry,
		},
	}

	vcap.StartComponent(r.component)

	return r
}

func (r *Router) RegisterComponent() {
	vcap.Register(r.component, r.mbusClient)
}

func (r *Router) subscribeRegistry(subject string, fn func(*registryMessage)) {
	callback := func(payload []byte) {
		var rm registryMessage

		err := json.Unmarshal(payload, &rm)
		if err != nil {
			lm := fmt.Sprintf("%s: Error unmarshalling JSON: %s", subject, err)
			log.Log(steno.LOG_WARN, lm, map[string]interface{}{"payload": string(payload)})
		}

		lm := fmt.Sprintf("%s: Received message", subject)
		log.Log(steno.LOG_DEBUG, lm, map[string]interface{}{"message": rm})

		fn(&rm)
	}
	r.mbusClient.Subscribe(subject, callback)
}

func (r *Router) SubscribeRegister() {
	r.subscribeRegistry("router.register", func(rm *registryMessage) {
		log.Infof("Got router.register: %v", rm)
		r.registry.Register(rm)
	})
}

func (r *Router) SubscribeUnregister() {
	r.subscribeRegistry("router.unregister", func(rm *registryMessage) {
		log.Infof("Got router.unregister: %v", rm)
		r.registry.Unregister(rm)
	})
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

func (r *Router) SendStartMessage() {
	host, err := vcap.LocalIP()
	if err != nil {
		panic(err)
	}
	d := vcap.RouterStart{vcap.GenerateUUID(), []string{host}}

	b, err := json.Marshal(d)
	if err != nil {
		panic(err)
	}

	// Send start message once at start
	r.mbusClient.Publish("router.start", b)

	go func() {
		t := time.NewTicker(r.config.PublishStartMessageInterval)

		for {
			select {
			case <-t.C:
				log.Info("Sending start message")
				r.mbusClient.Publish("router.start", b)
			}
		}
	}()
}

func (r *Router) Run() {
	var err error

	go func() {
		for {
			err = r.mbusClient.Connect()
			if err == nil {
				break
			}
			log.Errorf("Could not connect to NATS: ", err.Error())
			time.Sleep(500 * time.Millisecond)
		}
	}()

	r.RegisterComponent()

	// Kickstart sending start messages
	r.SendStartMessage()

	// Subscribe register/unregister router
	r.SubscribeRegister()
	r.SubscribeUnregister()

	// Schedule flushing active app's app_id
	r.ScheduleFlushApps()

	// Wait for one start message send interval, such that the router's registry
	// can be populated before serving requests.
	if r.config.PublishStartMessageInterval != 0 {
		log.Infof("Waiting %s before listening...", r.config.PublishStartMessageInterval)
		time.Sleep(r.config.PublishStartMessageInterval)
	}

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", r.config.Port))
	if err != nil {
		log.Fatalf("net.Listen: %s", err)
	}

	util.WritePidFile(r.config.Pidfile)

	log.Infof("Listening on %s", l.Addr())

	s := proxy.Server{Handler: r.proxy}

	err = s.Serve(l)
	if err != nil {
		log.Fatalf("proxy.Serve: %s", err)
	}

}

func (r *Router) establishMBus() {
	mbusClient, err := mbus.NewCFMessageBus("NATS")
	r.mbusClient = mbusClient
	if err != nil {
		panic("Could not connect to NATS")
	}

	host := r.config.Nats.Host
	user := r.config.Nats.User
	pass := r.config.Nats.Pass
	port := r.config.Nats.Port

	r.mbusClient.Configure(host, int(port), user, pass)
}
