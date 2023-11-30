package router

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"
)

type HealthListener struct {
	HealthCheck http.Handler
	TLSConfig   *tls.Config
	Port        uint16

	listener net.Listener
}

func (hl *HealthListener) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		hl.HealthCheck.ServeHTTP(w, req)
	})

	addr := fmt.Sprintf("0.0.0.0:%d", hl.Port)
	s := &http.Server{
		Addr:         addr,
		Handler:      mux,
		TLSConfig:    hl.TLSConfig,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	hl.listener = l

	go func() {
		if hl.TLSConfig != nil {
			err = s.ServeTLS(l, "", "")
		} else {
			err = s.Serve(l)
		}
	}()
	return nil
}

func (hl *HealthListener) Stop() {
	if hl.listener != nil {
		hl.listener.Close()
	}
}
