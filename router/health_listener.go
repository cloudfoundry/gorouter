package router

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	log "code.cloudfoundry.org/gorouter/logger"
)

type HealthListener struct {
	HealthCheck http.Handler
	TLSConfig   *tls.Config
	Port        uint16
	Router      *Router
	Logger      *slog.Logger

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
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	var err error
	hl.listener, err = net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	healthListener := hl.listener
	if hl.TLSConfig != nil {
		hl.tlsListener = tls.NewListener(hl.listener, hl.TLSConfig)
		healthListener = hl.tlsListener
	}
	go func() {
		err := s.Serve(healthListener)
		if !hl.Router.IsStopping() {
			hl.Logger.Error("health-listener-failed", log.ErrAttr(err))
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
