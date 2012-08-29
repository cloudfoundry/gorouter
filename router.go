package router

import (
	"code.google.com/p/go.net/websocket"
	"encoding/base64"
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	"log"
	"net/http"
	"runtime"
	"time"
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

	go r.startStatusHTTP()

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

func memStatsServer(ws *websocket.Conn) {
	var e error

	var t *time.Ticker = time.NewTicker(100 * time.Millisecond)
	defer t.Stop()

	enc := json.NewEncoder(ws)

	for {
		select {
		case <-t.C:
			var ms runtime.MemStats

			runtime.ReadMemStats(&ms)

			e = enc.Encode(ms)
			if e != nil {
				fmt.Printf("WebSocket error: %s\n", e)
				return
			}
		}
	}
}

func (r *Router) startStatusHTTP() {
	http.Handle("/ws", websocket.Handler(memStatsServer))
	http.Handle("/", http.FileServer(http.Dir(".")))

	http.HandleFunc("/data.json", func(w http.ResponseWriter, req *http.Request) {
		// var ms runtime.MemStats
		// runtime.ReadMemStats(&ms)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)

		enc := json.NewEncoder(w)
		enc.Encode(r.status)
	})

	http.ListenAndServe(fmt.Sprintf(":%d", config.StatusPort), nil)
}
