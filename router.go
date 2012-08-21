package router

import (
	"code.google.com/p/go.net/websocket"
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

	config Config
}

func NewRouter(c *Config) *Router {
	router := new(Router)

	router.config = *c
	router.proxy = NewProxy()
	router.natsClient = startNATS(c.Nats.Host, c.Nats.User, c.Nats.Pass)

	return router
}

func (r *Router) Run() {
	reg := r.natsClient.NewSubscription("router.register")
	reg.Subscribe()

	// Start message
	r.natsClient.Publish("router.start", []byte(""))

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

	go startStatusHTTP(r.config.StatusPort)

	err := http.ListenAndServe(fmt.Sprintf(":%d", r.config.Port), r.proxy)
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

func startStatusHTTP(port int) {
	http.Handle("/ws", websocket.Handler(memStatsServer))
	http.Handle("/", http.FileServer(http.Dir(".")))

	http.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		var ms runtime.MemStats

		runtime.ReadMemStats(&ms)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)

		enc := json.NewEncoder(w)
		enc.Encode(ms)
	})

	fmt.Printf("Starting...\n")
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
