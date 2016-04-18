package router

import (
	"io/ioutil"
	"os"
	"strconv"
	"sync"
	"syscall"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/gorouter/common"
	"github.com/cloudfoundry/gorouter/common/health"
	router_http "github.com/cloudfoundry/gorouter/common/http"
	"github.com/cloudfoundry/gorouter/common/schema"
	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/proxy"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/varz"
	"github.com/nats-io/nats"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/localip"

	"bytes"
	"compress/zlib"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

var DrainTimeout = errors.New("router: Drain timeout")

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

	logger  lager.Logger
	errChan chan error
}

func NewRouter(logger lager.Logger, cfg *config.Config, p proxy.Proxy, mbusClient *nats.Conn, r *registry.RouteRegistry,
	v varz.Varz, logCounter *schema.LogCounter, errChan chan error) (*Router, error) {

	var host string
	if cfg.Status.Port != 0 {
		host = fmt.Sprintf("%s:%d", cfg.Ip, cfg.Status.Port)
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

	component := &common.VcapComponent{
		Config:  cfg,
		Varz:    varz,
		Healthz: healthz,
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
		stopping:     false,
	}

	if err := router.component.Start(); err != nil {
		return nil, err
	}

	return router, nil
}

type gorouterHandler struct {
	handler http.Handler
	logger  lager.Logger
}

func (h *gorouterHandler) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	setRequestXVcapRequestId(req, h.logger)

	h.handler.ServeHTTP(res, req)
}

func setRequestXVcapRequestId(request *http.Request, logger lager.Logger) {
	uuid, err := common.GenerateUUID()
	if err == nil {
		request.Header.Set(router_http.VcapRequestIdHeader, uuid)
		if logger != nil {
			logger.Debug("vcap-request-id-header-set", lager.Data{router_http.VcapRequestIdHeader: uuid})
		}
	}
}

func (r *Router) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	r.registry.StartPruningCycle()

	r.RegisterComponent()

	// Subscribe register/unregister router
	r.SubscribeRegister()
	r.HandleGreetings()
	r.SubscribeUnregister()

	// Kickstart sending start messages
	r.SendStartMessage()

	r.mbusClient.Opts.ReconnectedCB = func(conn *nats.Conn) {
		r.logger.Info(fmt.Sprintf("Reconnecting to NATS server %s...", conn.Opts.Url))
		r.SendStartMessage()
	}

	// Schedule flushing active app's app_id
	r.ScheduleFlushApps()

	// Wait for one start message send interval, such that the router's registry
	// can be populated before serving requests.
	if r.config.StartResponseDelayInterval != 0 {
		r.logger.Info(fmt.Sprintf("Waiting %s before listening...", r.config.StartResponseDelayInterval))
		time.Sleep(r.config.StartResponseDelayInterval)
	}

	handler := gorouterHandler{handler: dropsonde.InstrumentedHandler(r.proxy), logger: r.logger}

	server := &http.Server{
		Handler:   &handler,
		ConnState: r.HandleConnState,
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

	// create pid file
	err = r.writePidFile(r.config.PidFile)
	if err != nil {
		return err
	}

	r.logger.Info("gorouter.started")

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
			r.logger.Error("Error occurred: ", err)
			r.DrainAndStop()
		}
	case sig := <-signals:
		go func() {
			for sig := range signals {
				r.logger.Info(
					"gorouter.signal.ignored",
					lager.Data{
						"signal": sig.String(),
					},
				)
			}
		}()
		if sig == syscall.SIGUSR1 {
			r.DrainAndStop()
		} else {
			r.Stop()
		}
	}
	r.logger.Info("gorouter.exited")
}

func (r *Router) DrainAndStop() {
	drainWait := r.config.DrainWait
	drainTimeout := r.config.DrainTimeout
	r.logger.Info(
		"gorouter.draining",
		lager.Data{
			"wait":    (drainWait).String(),
			"timeout": (drainTimeout).String(),
		},
	)

	r.Drain(drainWait, drainTimeout)

	r.Stop()
}

func (r *Router) serveHTTPS(server *http.Server, errChan chan error) error {
	if r.config.EnableSSL {
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{r.config.SSLCertificate},
			CipherSuites: r.config.CipherSuites,
		}

		tlsListener, err := tls.Listen("tcp", fmt.Sprintf(":%d", r.config.SSLPort), tlsConfig)
		if err != nil {
			r.logger.Fatal("tls.Listen: %s", err)
			return err
		}

		r.tlsListener = tlsListener
		r.logger.Info(fmt.Sprintf("Listening on %s", tlsListener.Addr()))

		go func() {
			err := server.Serve(tlsListener)
			r.stopLock.Lock()
			if !r.stopping {
				errChan <- err
			}
			r.stopLock.Unlock()
			close(r.tlsServeDone)
		}()
	}
	return nil
}

func (r *Router) serveHTTP(server *http.Server, errChan chan error) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", r.config.Port))
	if err != nil {
		r.logger.Fatal("net.Listen: %s", err)
		return err
	}

	r.listener = listener
	r.logger.Info(fmt.Sprintf("Listening on %s", listener.Addr()))

	go func() {
		err := server.Serve(listener)
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
	r.proxy.Drain()

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
	r.logger.Info(
		"gorouter.stopped",
		lager.Data{
			"took": time.Since(stoppingAt).String(),
		},
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

	r.listener.Close()

	if r.tlsListener != nil {
		r.tlsListener.Close()
		<-r.tlsServeDone
	}

	<-r.serveDone
}

func (r *Router) RegisterComponent() {
	r.component.Register(r.mbusClient)
}

func (r *Router) SubscribeRegister() {
	r.subscribeRegistry("router.register", func(registryMessage *RegistryMessage) {
		//r.logger.Debug("Got router.register:", lager.Data{"registry Message": registryMessage})

		for _, uri := range registryMessage.Uris {
			r.registry.Register(
				uri,
				registryMessage.makeEndpoint(),
			)
		}
	})
}

func (r *Router) SubscribeUnregister() {
	r.subscribeRegistry("router.unregister", func(registryMessage *RegistryMessage) {
		r.logger.Debug("Got router.unregister:", lager.Data{"registry Message": registryMessage})

		for _, uri := range registryMessage.Uris {
			r.registry.Unregister(
				uri,
				registryMessage.makeEndpoint(),
			)
		}
	})
}

func (r *Router) HandleGreetings() {
	r.mbusClient.Subscribe("router.greet", func(msg *nats.Msg) {
		if msg.Reply == "" {
			r.logger.Info(fmt.Sprintf("Received message with empty reply on subject %s", msg.Subject))
			return
		}

		response, _ := r.greetMessage()
		r.mbusClient.Publish(msg.Reply, response)
	})
}

func (r *Router) SendStartMessage() {
	b, err := r.greetMessage()
	if err != nil {
		panic(err)
	}

	// Send start message once at start
	err = r.mbusClient.Publish("router.start", b)
	if err != nil {
		r.logger.Error("failed-to-publish-greet-message", err)
	}
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

	r.logger.Debug("Debug Info", lager.Data{"Active apps": len(x), "message size:": len(z)})

	r.mbusClient.Publish("router.active_apps", z)
}

func (r *Router) greetMessage() ([]byte, error) {
	host, err := localip.LocalIP()
	if err != nil {
		return nil, err
	}

	d := common.RouterStart{
		Id:    r.component.Varz.UUID,
		Hosts: []string{host},
		MinimumRegisterIntervalInSeconds: r.config.StartResponseDelayIntervalInSeconds,
		PruneThresholdInSeconds:          r.config.DropletStaleThresholdInSeconds,
	}

	return json.Marshal(d)
}

func (r *Router) subscribeRegistry(subject string, successCallback func(*RegistryMessage)) {
	callback := func(message *nats.Msg) {
		payload := message.Data

		var msg RegistryMessage

		err := json.Unmarshal(payload, &msg)
		if err != nil {
			logMessage := fmt.Sprintf("%s: Error unmarshalling JSON (%d; %s): %s", subject, len(payload), payload, err)
			r.logger.Info(logMessage, lager.Data{"payload": string(payload)})
			return
		}

		//logMessage := fmt.Sprintf("%s: Received message", subject)
		//r.logger.Debug(logMessage, lager.Data{"message": msg})

		if !msg.ValidateMessage() {
			logMessage := fmt.Sprintf("%s: Unable to validate message. route_service_url must be https", subject)
			r.logger.Info(logMessage, lager.Data{"message": msg})
			return
		}

		successCallback(&msg)
	}

	_, err := r.mbusClient.Subscribe(subject, callback)
	if err != nil {
		r.logger.Error(fmt.Sprintf("Error subscribing to %s ", subject), err)
	}
}
