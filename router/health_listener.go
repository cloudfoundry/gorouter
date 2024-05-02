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

	listener    net.Listener
	tlsListener net.Listener
}

func (hl *HealthListener) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		hl.HealthCheck.ServeHTTP(w, req)
	})
	mux.HandleFunc("/is-process-alive-do-not-use-for-loadbalancing", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
		req.Close = true
	})

	addr := fmt.Sprintf("0.0.0.0:%d", hl.Port)
	s := &http.Server{
		Addr:         addr,
		Handler:      mux,
		TLSConfig:    hl.TLSConfig,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	var err error
	hl.listener, err = net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	go func() {
		if hl.TLSConfig != nil {
			hl.tlsListener = tls.NewListener(hl.listener, hl.TLSConfig)
			err = s.Serve(hl.tlsListener)
		} else {
			err = s.Serve(hl.listener)
		}
	}()
	return nil
}

func (hl *HealthListener) Stop() {
	if hl.listener != nil {
		hl.listener.Close()
	}
	if hl.tlsListener != nil {
		hl.tlsListener.Close()
	}
}
