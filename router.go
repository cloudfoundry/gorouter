package router

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	"net/http"
	vcap "router/common"
	"syscall"
	"time"
)

type Router struct {
	proxy      *Proxy
	natsClient *nats.Client
	varz       *Varz
	pidfile    *vcap.PidFile
	activeApps *AppList
}

func NewRouter() *Router {
	router := new(Router)

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

	// setup active apps list
	router.activeApps = NewAppList()

	// setup session encoder
	var se *SessionEncoder
	se, err = NewAESSessionEncoder([]byte(config.SessionKey), base64.StdEncoding)
	if err != nil {
		panic(err)
	}

	router.proxy = NewProxy(se, router.activeApps, router.varz)

	varz := &vcap.Varz{
		UniqueVarz: router.varz,
	}

	component := &vcap.VcapComponent{
		Type:        "Router",
		Index:       config.Index,
		Host:        host(),
		Credentials: []string{config.Status.User, config.Status.Password},
		Config:      config,
		Varz:        varz,
		Healthz:     "ok",
	}

	vcap.Register(component, router.natsClient)

	return router
}

func (r *Router) SubscribeRegister() {
	reg := r.natsClient.NewSubscription("router.register")
	reg.Subscribe()

	go func() {
		for m := range reg.Inbox {
			var rm registerMessage

			e := json.Unmarshal(m.Payload, &rm)
			if e != nil {
				log.Warnf("unable to unmarshal %s : %s", string(m.Payload), e)
				continue
			}

			log.Debugf("router.register: %#v", rm)
			r.proxy.Register(&rm)
		}
	}()
}

func (r *Router) SubscribeUnregister() {
	unreg := r.natsClient.NewSubscription("router.unregister")
	unreg.Subscribe()

	go func() {
		for m := range unreg.Inbox {
			var rm registerMessage

			e := json.Unmarshal(m.Payload, &rm)
			if e != nil {
				log.Warnf("unable to unmarshal %s : %s", string(m.Payload), e)
				continue
			}

			log.Debugf("router.unregister: %#v", rm)
			r.proxy.Unregister(&rm)
		}
	}()
}

func (r *Router) ScheduleAppsFlushing() {
	if config.FlushAppsInterval == 0 {
		return
	}

	go func() {
		for {
			time.Sleep(time.Duration(config.FlushAppsInterval) * time.Second)

			b, _ := r.activeApps.EncodeAndReset()
			log.Debugf("flushing active_apps, app size: %d, msg size: %d", r.activeApps.Size(), len(b))

			r.natsClient.Publish("router.active_apps", b)
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
	r.ScheduleAppsFlushing()

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
