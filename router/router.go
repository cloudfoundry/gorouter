package router

import (
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"bytes"
	"compress/zlib"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"code.cloudfoundry.org/gorouter/common"
	"code.cloudfoundry.org/gorouter/common/health"
	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/common/schema"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/metrics/monitor"
	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/varz"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/localip"
	"code.cloudfoundry.org/routing-api/models"
	"github.com/armon/go-proxyproto"
	"github.com/cloudfoundry/dropsonde"
	"github.com/nats-io/nats"
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
	logger           lager.Logger
	errChan          chan error
}

type RegistryMessage struct {
	Host                    string            `json:"host"`
	Port                    uint16            `json:"port"`
	Uris                    []route.Uri       `json:"uris"`
	Tags                    map[string]string `json:"tags"`
	App                     string            `json:"app"`
	StaleThresholdInSeconds int               `json:"stale_threshold_in_seconds"`
	RouteServiceUrl         string            `json:"route_service_url"`
	PrivateInstanceId       string            `json:"private_instance_id"`
	PrivateInstanceIndex    string            `json:"private_instance_index"`
}

func NewRouter(logger lager.Logger, cfg *config.Config, p proxy.Proxy, mbusClient *nats.Conn, r *registry.RouteRegistry,
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
	health := handlers.NewHealthcheck("", heartbeatOK, logger)
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
	logger  lager.Logger
}

func (h *gorouterHandler) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	// The X-Vcap-Request-Id must be set before the request is passed into the
	// dropsonde InstrumentedHandler
	router_http.SetVcapRequestIdHeader(req, h.logger)

	h.handler.ServeHTTP(res, req)
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
		natsUrl, err := url.Parse(conn.ConnectedUrl())
		natsHost := ""
		if err != nil {
			r.logger.Error("nats-url-parse-error", err)
		} else {
			natsHost = natsUrl.Host
		}

		r.logger.Info("nats-connection-reconnected", lager.Data{"nats-host": natsHost})
		r.SendStartMessage()
	}

	// Schedule flushing active app's app_id
	r.ScheduleFlushApps()

	lbOKDelay := r.config.StartResponseDelayInterval - r.config.LoadBalancerHealthyThreshold

	totalWaitDelay := r.config.LoadBalancerHealthyThreshold
	if lbOKDelay > 0 {
		totalWaitDelay = r.config.StartResponseDelayInterval
	}

	r.logger.Info(fmt.Sprintf("Waiting %s before listening...", totalWaitDelay),
		lager.Data{"route_registration_interval": r.config.StartResponseDelayInterval.String(),
			"load_balancer_healthy_threshold": r.config.LoadBalancerHealthyThreshold.String()})

	if lbOKDelay > 0 {
		r.logger.Debug(fmt.Sprintf("Sleeping for %d, before enabling /health endpoint", lbOKDelay))
		time.Sleep(lbOKDelay)
	}

	atomic.StoreInt32(r.HeartbeatOK, 1)
	r.logger.Debug("Gorouter reporting healthy")
	time.Sleep(r.config.LoadBalancerHealthyThreshold)

	r.logger.Info("completed-wait")

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
		r.logger.Info("gorouter.exited")
	}
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

		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", r.config.SSLPort))
		if err != nil {
			r.logger.Fatal("tcp-listener-error", err)
			return err
		}

		if r.config.EnablePROXY {
			listener = &proxyproto.Listener{
				Listener:           listener,
				ProxyHeaderTimeout: proxyProtocolHeaderTimeout,
			}
		}

		r.tlsListener = tls.NewListener(listener, tlsConfig)

		r.logger.Info("tls-listener-started", lager.Data{"address": r.tlsListener.Addr()})

		go func() {
			err := server.Serve(r.tlsListener)
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
		r.logger.Fatal("tcp-listener-error", err)
		return err
	}

	r.listener = listener
	if r.config.EnablePROXY {
		r.listener = &proxyproto.Listener{
			Listener:           listener,
			ProxyHeaderTimeout: proxyProtocolHeaderTimeout,
		}
	}

	r.logger.Info("tcp-listener-started", lager.Data{"address": r.listener.Addr()})

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
		r.logger.Info("unregister-route", lager.Data{"message": registryMessage})
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
		MinimumRegisterIntervalInSeconds: int(r.config.StartResponseDelayInterval.Seconds()),
		PruneThresholdInSeconds:          int(r.config.DropletStaleThreshold.Seconds()),
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

func (rm *RegistryMessage) makeEndpoint() *route.Endpoint {
	return route.NewEndpoint(
		rm.App,
		rm.Host,
		rm.Port,
		rm.PrivateInstanceId,
		rm.PrivateInstanceIndex,
		rm.Tags,
		rm.StaleThresholdInSeconds,
		rm.RouteServiceUrl,
		models.ModificationTag{})
}

func (rm *RegistryMessage) ValidateMessage() bool {
	return rm.RouteServiceUrl == "" || strings.HasPrefix(rm.RouteServiceUrl, "https")
}
