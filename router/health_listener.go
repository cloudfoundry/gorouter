package router

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
)

type HealthListener struct {
	HealthCheck http.Handler
	TLSConfig   *tls.Config
	Port        uint16
	Router      *Router
	Logger      logger.Logger

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
		// #nosec G104 - ignore errors when writing HTTP responses so we don't spam our logs during a DoS
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
			hl.Logger.Error("health-listener-failed", zap.Error(err))
		}
	}()
	return nil
}

func (hl *HealthListener) Stop() {
	if hl.listener != nil {
		err := hl.listener.Close()
		if err != nil {
			hl.Logger.Error("failed-closing-health-listener", zap.Error(err))
		}
	}
	if hl.tlsListener != nil {
		err := hl.tlsListener.Close()
		if err != nil {
			hl.Logger.Error("failed-closing-health-tls-listener", zap.Error(err))
		}
	}
}
