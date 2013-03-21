package common

import (
	"encoding/json"
	"net/http"
	_ "net/http/pprof"
	. "router/common/http"
)

func startStatusServer() {
	hs := http.NewServeMux()

	hs.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		enc := json.NewEncoder(w)
		enc.Encode(UpdateHealthz())
	})

	hs.HandleFunc("/varz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		enc := json.NewEncoder(w)
		enc.Encode(UpdateVarz())
	})

	f := func(user, password string) bool {
		return user == Component.Credentials[0] && password == Component.Credentials[1]
	}

	s := &http.Server{
		Addr:    Component.Host,
		Handler: &BasicAuth{hs, f},
	}

	s.ListenAndServe()
}
