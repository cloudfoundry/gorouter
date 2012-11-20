package router

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	"net/http"
	vcap "router/common"
	"runtime"
	"syscall"
	"time"
)

type Router struct {
	proxy      *Proxy
	natsClient *nats.Client
	varz       *Varz
	pidfile    *vcap.PidFile
	registry   *Registry
}

func NewRouter() *Router {
	router := new(Router)

	// setup no procs
	if config.GoMaxProcs != 0 {
		runtime.GOMAXPROCS(config.GoMaxProcs)
	}

	// setup pidfile
	pidfile, err := vcap.NewPidFile(config.Pidfile)
	if err != nil {
		panic(err)
	}
	pidfile.UnlinkOnSignal(syscall.SIGTERM, syscall.SIGINT)
	router.pidfile = pidfile

	// setup nats
	router.natsClient = startNATS(config.Nats.Host, config.Nats.User, config.Nats.Pass)

	// setup varz
	router.varz = NewVarz()

	// setup session encoder
	var se *SessionEncoder
	se, err = NewAESSessionEncoder([]byte(config.SessionKey), base64.StdEncoding)
	if err != nil {
		panic(err)
	}

	router.registry = NewRegistry()
	router.registry.varz = router.varz
	router.proxy = NewProxy(se, router.varz, router.registry)

	varz := &vcap.Varz{
		UniqueVarz: router.varz,
	}

	component := &vcap.VcapComponent{
		Type:        "Router",
		Index:       config.Index,
		Host:        host(),
		Credentials: []string{config.Status.User, config.Status.Password},
		Config:      config,
		Logger:      log,
		Varz:        varz,
		Healthz:     "ok",
	}

	vcap.Register(component, router.natsClient)

	return router
}

func (r *Router) SubscribeRegister() {
	s := r.natsClient.NewSubscription("router.register")
	s.Subscribe()

	go func() {
		for m := range s.Inbox {
			var rm registerMessage

			e := json.Unmarshal(m.Payload, &rm)
			if e != nil {
				log.Warnf("unable to unmarshal %s : %s", string(m.Payload), e)
				continue
			}

			log.Debugf("router.register: %#v", rm)
			r.registry.Register(&rm)
		}
	}()
}

func (r *Router) SubscribeUnregister() {
	s := r.natsClient.NewSubscription("router.unregister")
	s.Subscribe()

	go func() {
		for m := range s.Inbox {
			var rm registerMessage

			e := json.Unmarshal(m.Payload, &rm)
			if e != nil {
				log.Warnf("unable to unmarshal %s : %s", string(m.Payload), e)
				continue
			}

			log.Debugf("router.unregister: %#v", rm)
			r.registry.Unregister(&rm)
		}
	}()
}

func (r *Router) flushApps(t time.Time) {
	x := r.registry.ActiveSince(t)

	y, err := json.Marshal(x)
	if err != nil {
		log.Warnf("json.Marshal: %s", err)
		return
	}

	b := bytes.Buffer{}
	w := zlib.NewWriter(&b)
	w.Write(y)
	w.Close()

	z := b.Bytes()

	log.Debugf("Active apps: %d, message size: %d", len(x), len(z))

	r.natsClient.Publish("router.active_apps", z)
}

func (r *Router) ScheduleFlushApps() {
	if config.FlushAppsInterval == 0 {
		return
	}

	go func() {
		t := time.NewTicker(time.Duration(config.FlushAppsInterval) * time.Second)
		n := time.Now()

		for {
			select {
			case <-t.C:
				n_ := time.Now()
				r.flushApps(n)
				n = n_
			}
		}
	}()
}

func (r *Router) Run() {
	// Subscribe register/unregister router
	r.SubscribeRegister()
	r.SubscribeUnregister()

	// Start message
	r.natsClient.Publish("router.start", []byte(""))

	// Schedule flushing active app's app_id
	r.ScheduleFlushApps()

	err := http.ListenAndServe(fmt.Sprintf(":%d", config.Port), r.proxy)
	if err != nil {
		log.Fatalf("ListenAndServe %s", err)
	}
}

func startNATS(host, user, pass string) *nats.Client {
	c := nats.NewClient()

	go func() {
		e := c.RunWithDefaults(host, user, pass)
		log.Fatalf("Failed to connect to nats server: %s", e.Error())
	}()

	return c
}

func host() string {
	if config.Status.Port == 0 {
		return ""
	}

	ip, err := vcap.LocalIP()
	if err != nil {
		panic(err)
	}

	return fmt.Sprintf("%s:%d", ip, config.Status.Port)
}
