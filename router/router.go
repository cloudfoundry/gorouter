package router

import (
	"bytes"
	"compress/zlib"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/armon/go-proxyproto"
	"github.com/nats-io/nats.go"

	"code.cloudfoundry.org/gorouter/common"
	"code.cloudfoundry.org/gorouter/common/health"
	"code.cloudfoundry.org/gorouter/common/schema"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics/monitor"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/varz"
)

var DrainTimeout = errors.New("router: Drain timeout")

const (
	emitInterval               = 1 * time.Second
	proxyProtocolHeaderTimeout = 100 * time.Millisecond
)

//go:generate counterfeiter -o ../fakes/route_services_server.go --fake-name RouteServicesServer . rss
type rss interface {
	Serve(handler http.Handler, errChan chan error) error
	Stop() error
}
type Router struct {
	config            *config.Config
	handler           http.Handler
	mbusClient        *nats.Conn
	registry          *registry.RouteRegistry
	varz              varz.Varz
	component         *common.VcapComponent
	routesListener    *RoutesListener
	healthListener    *HealthListener
	healthTLSListener *HealthListener

	listener            net.Listener
	tlsListener         net.Listener
	closeConnections    bool
	connLock            sync.Mutex
	idleConns           map[net.Conn]struct{}
	activeConns         map[net.Conn]struct{}
	drainDone           chan struct{}
	serveDone           chan struct{}
	tlsServeDone        chan struct{}
	stopping            bool
	stopLock            sync.Mutex
	uptimeMonitor       *monitor.Uptime
	health              *health.Health
	logger              *slog.Logger
	errChan             chan error
	routeServicesServer rss
}

func NewRouter(
	logger *slog.Logger,
	cfg *config.Config,
	handler http.Handler,
	mbusClient *nats.Conn,
	r *registry.RouteRegistry,
	v varz.Varz,
	h *health.Health,
	logCounter *schema.LogCounter,
	errChan chan error,
	routeServicesServer rss,
) (*Router, error) {
	var host string
	if cfg.Status.Port != 0 {
		host = fmt.Sprintf("%s:%d", cfg.Status.Host, cfg.Status.Port)
	}

	routerErrChan := errChan
	if routerErrChan == nil {
		routerErrChan = make(chan error, 3)
	}

	routesListener := &RoutesListener{
		Config:        cfg,
		RouteRegistry: r,
	}
	if err := routesListener.ListenAndServe(); err != nil {
		return nil, err
	}

	router := &Router{
		config:              cfg,
		handler:             handler,
		mbusClient:          mbusClient,
		registry:            r,
		varz:                v,
		routesListener:      routesListener,
		serveDone:           make(chan struct{}),
		tlsServeDone:        make(chan struct{}),
		idleConns:           make(map[net.Conn]struct{}),
		activeConns:         make(map[net.Conn]struct{}),
		logger:              logger,
		errChan:             routerErrChan,
		health:              h,
		stopping:            false,
		routeServicesServer: routeServicesServer,
	}

	healthCheck := handlers.NewHealthcheck(h, logger)
	if cfg.Status.EnableNonTLSHealthChecks {
		// TODO: remove all vcapcomponent logic in Summer 2026
		if cfg.Status.EnableDeprecatedVarzHealthzEndpoints {
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

			router.component = &common.VcapComponent{
				Config: cfg,
				Varz:   varz,
				Health: healthCheck,
				InfoRoutes: map[string]json.Marshaler{
					"/routes": r,
				},
				Logger: logger,
			}
		} else {
			router.healthListener = &HealthListener{
				Port:        cfg.Status.Port,
				HealthCheck: healthCheck,
				Router:      router,
				Logger:      logger.With("session", "nontls-health-listener"),
			}
			if err := router.healthListener.ListenAndServe(); err != nil {
				return nil, err
			}
		}
	}

	if len(cfg.Status.TLSCert.Certificate) != 0 {
		router.healthTLSListener = &HealthListener{
			Port: cfg.Status.TLS.Port,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{cfg.Status.TLSCert},
				CipherSuites: cfg.CipherSuites,
				MinVersion:   cfg.MinTLSVersion,
				MaxVersion:   cfg.MaxTLSVersion,
			},
			HealthCheck: healthCheck,
			Router:      router,
			Logger:      logger.With("session", "tls-health-listener"),
		}
		if cfg.EnableHTTP2 {
			router.healthTLSListener.TLSConfig.NextProtos = []string{"h2", "http/1.1"}
		}

		if err := router.healthTLSListener.ListenAndServe(); err != nil {
			return nil, err
		}
	}

	if router.healthListener == nil && router.component == nil && router.healthTLSListener == nil {
		return nil, fmt.Errorf("No TLS certificates provided and non-tls health listener disabled. No health listener can start. This is a bug in gorouter. This error should have been caught when parsing the config")
	}

	if router.component != nil {
		if err := router.component.Start(); err != nil {
			return nil, err
		}
	}

	router.uptimeMonitor = monitor.NewUptime(emitInterval, router.logger)
	return router, nil
}

// golang's default was 1mb. We want to make this explicit, so that we're able to create access logs via our own handler to process MAX_HEADER_BYTES
const MAX_HEADER_BYTES = 1024 * 1024

func (r *Router) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	r.registry.StartPruningCycle()

	err := r.RegisterComponent()
	if err != nil {
		return err
	}

	// Schedule flushing active app's app_id
	r.ScheduleFlushApps()

	r.logger.Debug("Sleeping before returning success on /health endpoint to preload routing table", slog.Float64("sleep_time_seconds", r.config.StartResponseDelayInterval.Seconds()))
	time.Sleep(r.config.StartResponseDelayInterval)

	server := &http.Server{
		Handler:        r.handler,
		ConnState:      r.HandleConnState,
		IdleTimeout:    r.config.FrontendIdleTimeout,
		MaxHeaderBytes: MAX_HEADER_BYTES,
	}

	err = r.serveHTTP(server, r.errChan)
	if err != nil {
		r.errChan <- err
		return err
	}
	err = r.serveHTTPS(server, r.errChan)
	if err != nil {
		r.errChan <- err
		return err
	}
	err = r.routeServicesServer.Serve(r.handler, r.errChan)
	if err != nil {
		r.errChan <- err
		return err
	}

	r.logger.Info("gorouter.started")
	go r.uptimeMonitor.Start()

	close(ready)

	r.OnErrOrSignal(signals, r.errChan)

	return nil
}

func (r *Router) OnErrOrSignal(signals <-chan os.Signal, errChan chan error) {
	select {
	case err := <-errChan:
		if err != nil {
			r.logger.Error("Error occurred", log.ErrAttr(err))
			r.health.SetHealth(health.Degraded)
		}
	case sig := <-signals:
		go func() {
			for sig := range signals {
				r.logger.Info(
					"gorouter.signal.ignored",
					slog.String("signal", sig.String()),
				)
			}
		}()
		if sig == syscall.SIGUSR1 {
			r.health.SetHealth(health.Degraded)
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
		slog.Float64("wait_seconds", drainWait.Seconds()),
		slog.Float64("timeout_seconds", drainTimeout.Seconds()),
	)

	err := r.Drain(drainWait, drainTimeout)
	if err != nil {
		r.logger.Error("gorouter-draining-error", log.ErrAttr(err))
	}

	r.Stop()
}

func (r *Router) serveHTTPS(server *http.Server, errChan chan error) error {
	if !r.config.EnableSSL {
		r.logger.Info("tls-listener-not-enabled")
		return nil
	}

	tlsConfig := &tls.Config{
		Certificates: r.config.SSLCertificates,
		CipherSuites: r.config.CipherSuites,
		MinVersion:   r.config.MinTLSVersion,
		MaxVersion:   r.config.MaxTLSVersion,
		ClientCAs:    r.config.ClientCAPool,
		ClientAuth:   r.config.ClientCertificateValidation,
	}

	if r.config.VerifyClientCertificatesBasedOnProvidedMetadata && r.config.VerifyClientCertificateMetadataRules != nil {
		tlsConfig.VerifyPeerCertificate = r.verifyMtlsMetadata
	}

	if r.config.EnableHTTP2 {
		tlsConfig.NextProtos = []string{"h2", "http/1.1"}
	}

	// Although this functionality is deprecated there is no intention to remove it from the stdlib
	// due to the Go 1 compatibility promise. We rely on it to prefer more specific matches (a full
	// SNI match over wildcard matches) instead of relying on the order of certificates.
	//lint:ignore SA1019 - see ^^
	tlsConfig.BuildNameToCertificate()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", r.config.SSLPort))
	if err != nil {
		log.Fatal(r.logger, "tls-listener-error", log.ErrAttr(err))
		return err
	}

	if r.config.EnablePROXY {
		listener = &proxyproto.Listener{
			Listener:           listener,
			ProxyHeaderTimeout: proxyProtocolHeaderTimeout,
		}
	}

	r.tlsListener = tls.NewListener(listener, tlsConfig)

	r.logger.Info("tls-listener-started", slog.Any("address", log.StructValue(r.tlsListener.Addr())))

	go func() {
		err := server.Serve(r.tlsListener)
		r.stopLock.Lock()
		if !r.stopping {
			errChan <- err
		}
		r.stopLock.Unlock()
		close(r.tlsServeDone)
	}()
	return nil
}

// verifyMtlsMetadata checks the Config.VerifyClientCertificateMetadataRules rules, if any are defined.
//
// Returns an error if one of the applicable verification rules fails.
func (r *Router) verifyMtlsMetadata(_ [][]byte, chains [][]*x509.Certificate) error {
	if chains != nil {
		return config.VerifyClientCertMetadata(r.config.VerifyClientCertificateMetadataRules, chains, r.logger)
	}
	return nil
}

func (r *Router) serveHTTP(server *http.Server, errChan chan error) error {
	if r.config.DisableHTTP {
		r.logger.Info("tcp-listener-disabled")
		return nil
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", r.config.Port))
	if err != nil {
		log.Fatal(r.logger, "tcp-listener-error", log.ErrAttr(err))
		return err
	}

	r.listener = listener
	if r.config.EnablePROXY {
		r.listener = &proxyproto.Listener{
			Listener:           listener,
			ProxyHeaderTimeout: proxyProtocolHeaderTimeout,
		}
	}

	r.logger.Info("tcp-listener-started", slog.Any("address", log.StructValue(r.listener.Addr())))

	go func() {
		err := server.Serve(r.listener)
		r.stopLock.Lock()
		if !r.stopping {
			errChan <- err
		}
		r.stopLock.Unlock()

		close(r.serveDone)
	}()
	return nil
}

func (r *Router) Drain(drainWait, drainTimeout time.Duration) error {
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

func (r *Router) IsStopping() bool {
	r.stopLock.Lock()
	defer r.stopLock.Unlock()
	return r.stopping
}

func (r *Router) Stop() {
	stoppingAt := time.Now()

	r.logger.Info("gorouter.stopping")

	r.stopListening()

	r.connLock.Lock()
	r.closeIdleConns()
	r.connLock.Unlock()

	if r.component != nil {
		err := r.component.Stop()
		if err != nil {
			r.logger.Error("error-stopping-component", log.ErrAttr(err))
		}
	}
	if r.healthListener != nil {
		r.healthListener.Stop()
	}
	err := r.routesListener.Stop()
	if err != nil {
		r.logger.Error("error-stopping-route-listener", log.ErrAttr(err))
	}
	if r.healthTLSListener != nil {
		r.healthTLSListener.Stop()
	}
	r.uptimeMonitor.Stop()
	r.logger.Info(
		"gorouter.stopped",
		slog.Duration("took", time.Since(stoppingAt)),
	)
}

// connLock must be locked
func (r *Router) closeIdleConns() {
	r.closeConnections = true

	for conn := range r.idleConns {
		// #nosec G104 - ignore connection close errors here since this has the potential to balloon logs up
		conn.Close()
	}
}

func (r *Router) stopListening() {
	r.stopLock.Lock()
	r.stopping = true
	r.stopLock.Unlock()
	var err error

	if r.listener != nil {
		err = r.listener.Close()
		if err != nil {
			r.logger.Error("error-stopping-route-services-server", log.ErrAttr(err))
		}
		<-r.serveDone
	}

	if r.tlsListener != nil {
		err = r.tlsListener.Close()
		if err != nil {
			r.logger.Error("error-stopping-route-services-server", log.ErrAttr(err))
		}
		<-r.tlsServeDone
	}

	err = r.routeServicesServer.Stop()
	if err != nil {
		r.logger.Error("error-stopping-route-services-server", log.ErrAttr(err))
	}
}

func (r *Router) RegisterComponent() error {
	if r.component != nil {
		return r.component.Register(r.mbusClient)
	}
	return nil
}

func (r *Router) ScheduleFlushApps() {
	if r.config.PublishActiveAppsInterval == 0 {
		return
	}

	go func() {
		t := time.NewTicker(r.config.PublishActiveAppsInterval)
		x := time.Now()

		for {
			<-t.C
			y := time.Now()
			r.flushApps(x)
			x = y
		}
	}()
}

func (r *Router) HandleConnState(conn net.Conn, state http.ConnState) {
	r.connLock.Lock()

	switch state {
	case http.StateActive:
		r.activeConns[conn] = struct{}{}
		delete(r.idleConns, conn)
	case http.StateIdle:
		delete(r.activeConns, conn)
		r.idleConns[conn] = struct{}{}

		if r.closeConnections {
			// #nosec G104 - ignore connection close errors here since this has the potential to balloon logs up
			conn.Close()
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
	_, err = w.Write(y)
	if err != nil {
		r.logger.Error("error-compressing-active-apps-message", log.ErrAttr(err))
	}
	err = w.Close()
	if err != nil {
		r.logger.Error("error-closing-compression-writer", log.ErrAttr(err))
	}

	z := b.Bytes()

	r.logger.Debug("Debug Info", slog.Int("Active apps", len(x)), slog.Int("message size:", len(z)))

	err = r.mbusClient.Publish("router.active_apps", z)
	if err != nil {
		r.logger.Error("error-publishing-active-apps-to-nats", log.ErrAttr(err))
	}
}
