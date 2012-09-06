package router

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	"log"
	"net/http"
	vcap "router/common"
)

type Router struct {
	proxy      *Proxy
	natsClient *nats.Client
	status     *ServerStatus
}

func NewRouter() *Router {
	router := new(Router)

	router.natsClient = startNATS(config.Nats.Host, config.Nats.User, config.Nats.Pass)
	router.status = NewServerStatus()

	se, err := NewAESSessionEncoder([]byte(config.SessionKey), base64.StdEncoding)
	if err != nil {
		panic(err)
	}

	router.proxy = NewProxy(se)
	router.proxy.status = router.status

	component := &vcap.VcapComponent{
		Type:        "Router",
		Index:       config.Index,
		Host:        host(),
		Credentials: []string{config.Status.User, config.Status.Password},
		Varz:        router.status,
		Healthz:     "ok",
		Config:      config,
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
				// TODO: maybe logger
				continue
			}

			// TODO: use logger
			fmt.Printf("router.register: %#v\n", rm)
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
				// TODO: maybe logger
				continue
			}

			// TODO: use logger
			fmt.Printf("router.unregister: %#v\n", rm)
			r.proxy.Unregister(&rm)
		}
	}()
}

func (r *Router) Run() {
	r.SubscribeRegister()
	r.SubscribeUnregister()

	// Start message
	r.natsClient.Publish("router.start", []byte(""))

	err := http.ListenAndServe(fmt.Sprintf(":%d", config.Port), r.proxy)
	if err != nil {
		log.Panic("ListenAndServe ", err)
	}
}

func startNATS(host, user, pass string) *nats.Client {
	c := nats.NewClient()

	go func() {
		e := c.RunWithDefaults(host, user, pass)
		panic(e)
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
