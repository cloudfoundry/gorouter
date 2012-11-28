package common

import (
	"encoding/json"
	"net/http"
	. "router/common/http"
)

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

	f := func(user, password string) bool {
		return user == Component.Credentials[0] && password == Component.Credentials[1]
	}

	server := &http.Server{
		Addr:    Component.Host,
		Handler: &BasicAuth{mux, f},
	}

	server.ListenAndServe()
}
