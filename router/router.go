package router

import (
	"io/ioutil"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"

	"bytes"
	"compress/zlib"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/common"
	"code.cloudfoundry.org/gorouter/common/health"
	"code.cloudfoundry.org/gorouter/common/schema"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics/monitor"
	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/varz"
	"github.com/armon/go-proxyproto"
	"github.com/cloudfoundry/dropsonde"
	"github.com/nats-io/nats"
	"github.com/uber-go/zap"
)

var DrainTimeout = errors.New("router: Drain timeout")

const (
	emitInterval               = 1 * time.Second
	proxyProtocolHeaderTimeout = 100 * time.Millisecond
)

var noDeadline = time.Time{}

type Router struct {
	config     *config.Config
	proxy      proxy.Proxy
	mbusClient *nats.Conn
	registry   *registry.RouteRegistry
	varz       varz.Varz
	component  *common.VcapComponent

	listener         net.Listener
	tlsListener      net.Listener
	closeConnections bool
	connLock         sync.Mutex
	idleConns        map[net.Conn]struct{}
	activeConns      map[net.Conn]struct{}
	drainDone        chan struct{}
	serveDone        chan struct{}
	tlsServeDone     chan struct{}
	stopping         bool
	stopLock         sync.Mutex
	uptimeMonitor    *monitor.Uptime
	HeartbeatOK      *int32
	logger           logger.Logger
	errChan          chan error
	NatsHost         *atomic.Value
}

func NewRouter(logger logger.Logger, cfg *config.Config, p proxy.Proxy, mbusClient *nats.Conn, r *registry.RouteRegistry,
	v varz.Varz, heartbeatOK *int32, logCounter *schema.LogCounter, errChan chan error) (*Router, error) {

	var host string
	if cfg.Status.Port != 0 {
		host = fmt.Sprintf("%s:%d", cfg.Status.Host, cfg.Status.Port)
	}

	varz := &health.Varz{
		UniqueVarz: v,
		GenericVarz: health.GenericVarz{
			Type:        "Router",
			Index:       cfg.Index,
			Host:        host,
			Credentials: []string{cfg.Status.User, cfg.Status.Pass},
			LogCounts:   logCounter,
		},
	}

	healthz := &health.Healthz{}
	health := handlers.NewHealthcheck(heartbeatOK, logger)
	component := &common.VcapComponent{
		Config:  cfg,
		Varz:    varz,
		Healthz: healthz,
		Health:  health,
		InfoRoutes: map[string]json.Marshaler{
			"/routes": r,
		},
		Logger: logger,
	}

	routerErrChan := errChan
	if routerErrChan == nil {
		routerErrChan = make(chan error, 2)
	}

	router := &Router{
		config:       cfg,
		proxy:        p,
		mbusClient:   mbusClient,
		registry:     r,
		varz:         v,
		component:    component,
		serveDone:    make(chan struct{}),
		tlsServeDone: make(chan struct{}),
		idleConns:    make(map[net.Conn]struct{}),
		activeConns:  make(map[net.Conn]struct{}),
		logger:       logger,
		errChan:      routerErrChan,
		HeartbeatOK:  heartbeatOK,
		stopping:     false,
	}

	if err := router.component.Start(); err != nil {
		return nil, err
	}

	router.uptimeMonitor = monitor.NewUptime(emitInterval)
	return router, nil
}

type gorouterHandler struct {
	handler http.Handler
	logger  logger.Logger
}

func (h *gorouterHandler) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	h.handler.ServeHTTP(res, req)
}

func (r *Router) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	r.registry.StartPruningCycle()

	r.RegisterComponent()

	// Schedule flushing active app's app_id
	r.ScheduleFlushApps()

	r.logger.Debug("Sleeping before returning success on /health endpoint to preload routing table", zap.Float64("sleep_time_seconds", r.config.StartResponseDelayInterval.Seconds()))
	time.Sleep(r.config.StartResponseDelayInterval)

	handler := gorouterHandler{handler: dropsonde.InstrumentedHandler(r.proxy), logger: r.logger}

	server := &http.Server{
		Handler:     &handler,
		ConnState:   r.HandleConnState,
		IdleTimeout: 5 * time.Second,
	}

	err := r.serveHTTP(server, r.errChan)
	if err != nil {
		r.errChan <- err
		return err
	}
	err = r.serveHTTPS(server, r.errChan)
	if err != nil {
		r.errChan <- err
		return err
	}

	atomic.StoreInt32(r.HeartbeatOK, 1)
	// create pid file
	err = r.writePidFile(r.config.PidFile)
	if err != nil {
		return err
	}

	r.logger.Info("gorouter.started")
	go r.uptimeMonitor.Start()

	close(ready)

	r.OnErrOrSignal(signals, r.errChan)

	return nil
}

func (r *Router) writePidFile(pidFile string) error {
	if pidFile != "" {
		pid := strconv.Itoa(os.Getpid())
		err := ioutil.WriteFile(pidFile, []byte(pid), 0660)
		if err != nil {
			return fmt.Errorf("cannot create pid file:  %v", err)
		}
	}
	return nil
}

func (r *Router) OnErrOrSignal(signals <-chan os.Signal, errChan chan error) {
	select {
	case err := <-errChan:
		if err != nil {
			r.logger.Error("Error occurred", zap.Error(err))
			r.DrainAndStop()
		}
	case sig := <-signals:
		go func() {
			for sig := range signals {
				r.logger.Info(
					"gorouter.signal.ignored",
					zap.String("signal", sig.String()),
				)
			}
		}()
		if sig == syscall.SIGUSR1 {
			r.DrainAndStop()
		} else {
			r.Stop()
		}
		r.logger.Info("gorouter.exited")
	}
}

func (r *Router) DrainAndStop() {
	drainWait := r.config.DrainWait
	drainTimeout := r.config.DrainTimeout
	r.logger.Info(
		"gorouter-draining",
		zap.Float64("wait_seconds", drainWait.Seconds()),
		zap.Float64("timeout_seconds", drainTimeout.Seconds()),
	)

	r.Drain(drainWait, drainTimeout)

	r.Stop()
}

func (r *Router) serveHTTPS(server *http.Server, errChan chan error) error {
	if r.config.EnableSSL {
		rootCAs, err := x509.SystemCertPool()
		if err != nil {
			rootCAs = nil
		}
		if err == nil {
			for _, cert := range r.config.CACerts {
				if ok := rootCAs.AppendCertsFromPEM([]byte(cert)); !ok {
					r.logger.Fatal("servehttps-certpool-error",
						zap.Error(fmt.Errorf("error adding a CA cert to cert pool")))
				}
			}
		}

		tlsConfig := &tls.Config{
			Certificates: r.config.SSLCertificates,
			CipherSuites: r.config.CipherSuites,
			MinVersion:   r.config.MinTLSVersion,
			ClientCAs:    rootCAs,
			ClientAuth:   tls.VerifyClientCertIfGiven,
		}

		tlsConfig.BuildNameToCertificate()

		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", r.config.SSLPort))
		if err != nil {
			r.logger.Fatal("tls-listener-error", zap.Error(err))
			return err
		}

		if r.config.EnablePROXY {
			listener = &proxyproto.Listener{
				Listener:           listener,
				ProxyHeaderTimeout: proxyProtocolHeaderTimeout,
			}
		}

		r.tlsListener = tls.NewListener(listener, tlsConfig)

		r.logger.Info("tls-listener-started", zap.Object("address", r.tlsListener.Addr()))

		go func() {
			err := server.Serve(r.tlsListener)
			r.stopLock.Lock()
			if !r.stopping {
				errChan <- err
			}
			r.stopLock.Unlock()
			close(r.tlsServeDone)
		}()
	} else {
		r.logger.Info("tls-listener-not-enabled")
	}
	return nil
}

func (r *Router) serveHTTP(server *http.Server, errChan chan error) error {
	if !r.config.DisableHTTP {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", r.config.Port))
		if err != nil {
			r.logger.Fatal("tcp-listener-error", zap.Error(err))
			return err
		}

		r.listener = listener
		if r.config.EnablePROXY {
			r.listener = &proxyproto.Listener{
				Listener:           listener,
				ProxyHeaderTimeout: proxyProtocolHeaderTimeout,
			}
		}

		r.logger.Info("tcp-listener-started", zap.Object("address", r.listener.Addr()))

		go func() {
			err := server.Serve(r.listener)
			r.stopLock.Lock()
			if !r.stopping {
				errChan <- err
			}
			r.stopLock.Unlock()

			close(r.serveDone)
		}()
	} else {
		r.logger.Info("tcp-listener-disabled")
	}
	return nil
}

func (r *Router) Drain(drainWait, drainTimeout time.Duration) error {
	atomic.StoreInt32(r.HeartbeatOK, 0)

	<-time.After(drainWait)

	r.stopListening()

	drained := make(chan struct{})

	r.connLock.Lock()

	r.logger.Info(fmt.Sprintf("Draining with %d outstanding active connections", len(r.activeConns)))
	r.logger.Info(fmt.Sprintf("Draining with %d outstanding idle connections", len(r.idleConns)))
	r.closeIdleConns()

	if len(r.activeConns) == 0 {
		close(drained)
	} else {
		r.drainDone = drained
	}

	r.connLock.Unlock()

	select {
	case <-drained:
	case <-time.After(drainTimeout):
		r.logger.Info("router.drain.timed-out")
		return DrainTimeout
	}

	return nil
}

func (r *Router) Stop() {
	stoppingAt := time.Now()

	r.logger.Info("gorouter.stopping")

	r.stopListening()

	r.connLock.Lock()
	r.closeIdleConns()
	r.connLock.Unlock()

	r.component.Stop()
	r.uptimeMonitor.Stop()
	r.logger.Info(
		"gorouter.stopped",
		zap.Duration("took", time.Since(stoppingAt)),
	)
}

// connLock must be locked
func (r *Router) closeIdleConns() {
	r.closeConnections = true

	for conn, _ := range r.idleConns {
		conn.Close()
	}
}

func (r *Router) stopListening() {
	r.stopLock.Lock()
	r.stopping = true
	r.stopLock.Unlock()

	if r.listener != nil {
		r.listener.Close()
		<-r.serveDone
	}

	if r.tlsListener != nil {
		r.tlsListener.Close()
		<-r.tlsServeDone
	}
}

func (r *Router) RegisterComponent() {
	r.component.Register(r.mbusClient)
}

func (r *Router) ScheduleFlushApps() {
	if r.config.PublishActiveAppsInterval == 0 {
		return
	}

	go func() {
		t := time.NewTicker(r.config.PublishActiveAppsInterval)
		x := time.Now()

		for {
			select {
			case <-t.C:
				y := time.Now()
				r.flushApps(x)
				x = y
			}
		}
	}()
}

func (r *Router) HandleConnState(conn net.Conn, state http.ConnState) {
	endpointTimeout := r.config.EndpointTimeout

	r.connLock.Lock()

	switch state {
	case http.StateActive:
		r.activeConns[conn] = struct{}{}
		delete(r.idleConns, conn)

		conn.SetDeadline(noDeadline)
	case http.StateIdle:
		delete(r.activeConns, conn)
		r.idleConns[conn] = struct{}{}

		if r.closeConnections {
			conn.Close()
		} else {
			deadline := noDeadline
			if endpointTimeout > 0 {
				deadline = time.Now().Add(endpointTimeout)
			}

			conn.SetDeadline(deadline)
		}
	case http.StateHijacked, http.StateClosed:
		i := len(r.idleConns)
		delete(r.idleConns, conn)
		if i == len(r.idleConns) {
			delete(r.activeConns, conn)
		}
	}

	if r.drainDone != nil && len(r.activeConns) == 0 {
		close(r.drainDone)
		r.drainDone = nil
	}

	r.connLock.Unlock()
}

func (r *Router) flushApps(t time.Time) {
	x := r.varz.ActiveApps().ActiveSince(t)

	y, err := json.Marshal(x)
	if err != nil {
		r.logger.Info(fmt.Sprintf("flushApps: Error marshalling JSON: %s", err.Error()))
		return
	}

	b := bytes.Buffer{}
	w := zlib.NewWriter(&b)
	w.Write(y)
	w.Close()

	z := b.Bytes()

	r.logger.Debug("Debug Info", zap.Int("Active apps", len(x)), zap.Int("message size:", len(z)))

	r.mbusClient.Publish("router.active_apps", z)
}
