package router

import (
	"code.google.com/p/go.net/websocket"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

type BasicAuth struct {
	handler http.Handler
}

func (r *Router) startStatusHTTP() {
	mux := http.NewServeMux()
	mux.HandleFunc("/data.json", func(w http.ResponseWriter, req *http.Request) {
		// var ms runtime.MemStats
		// runtime.ReadMemStats(&ms)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)

		enc := json.NewEncoder(w)
		enc.Encode(r.status)
	})
	// mux.Handle("/ws", websocket.Handler(memStatsServer))
	// mux.Handle("/", http.FileServer(http.Dir(".")))

	basicAuth := &BasicAuth{
		handler: mux,
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.Status.Port),
		Handler: basicAuth,
	}

	server.ListenAndServe()
}

func (a *BasicAuth) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !checkAuth(req, config.Status.User, config.Status.Password) {
		w.Header().Set("WWW-Authenticate", "Basic")
		w.WriteHeader(401)
		w.Write([]byte("401 Unauthorized\n"))
	} else {
		a.handler.ServeHTTP(w, req)
	}
}

func checkAuth(req *http.Request, user string, password string) bool {
	if user == "" && password == "" {
		return true
	}

	authParts := strings.Split(req.Header.Get("Authorization"), " ")
	if len(authParts) != 2 || authParts[0] != "Basic" {
		return false
	}

	code, err := base64.StdEncoding.DecodeString(authParts[1])
	if err != nil {
		return false
	}

	userPass := strings.Split(string(code), ":")
	if len(userPass) != 2 || userPass[0] != user || userPass[1] != password {
		return false
	}
	return true
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
