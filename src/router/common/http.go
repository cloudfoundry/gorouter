package common

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

type BasicAuth struct {
	handler http.Handler
}

func (a *BasicAuth) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !checkAuth(req, Component.Credentials[0], Component.Credentials[1]) {
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

func startStatusServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)

		enc := json.NewEncoder(w)
		enc.Encode(UpdateHealthz())
	})

	mux.HandleFunc("/varz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)

		enc := json.NewEncoder(w)
		enc.Encode(UpdateVarz())
	})

	basicAuth := &BasicAuth{
		handler: mux,
	}

	server := &http.Server{
		Addr:    Component.Host,
		Handler: basicAuth,
	}

	server.ListenAndServe()
}
