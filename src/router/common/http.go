package common

import (
	"encoding/json"
	"net/http"
	. "router/common/http"
)

func init() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		enc := json.NewEncoder(w)
		enc.Encode(UpdateHealthz())
	})

	http.HandleFunc("/varz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		enc := json.NewEncoder(w)
		enc.Encode(UpdateVarz())
	})
}

func startStatusServer() {
	f := func(user, password string) bool {
		return user == Component.Credentials[0] && password == Component.Credentials[1]
	}

	s := &http.Server{
		Addr:    Component.Host,
		Handler: &BasicAuth{http.DefaultServeMux, f},
	}

	s.ListenAndServe()
}
