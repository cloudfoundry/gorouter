package router

import (
	"sync"

	"github.com/apcera/nats"
	"github.com/cloudfoundry/dropsonde"
	vcap "github.com/cloudfoundry/gorouter/common"
	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/proxy"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/varz"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/yagnats"
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
	mbusClient yagnats.NATSConn
	registry   *registry.RouteRegistry
	varz       varz.Varz
	component  *vcap.VcapComponent

	listener         net.Listener
	tlsListener      net.Listener
	closeConnections bool
	connLock         sync.Mutex
	idleConns        map[net.Conn]struct{}
	activeConns      map[net.Conn]struct{}
	drainDone        chan struct{}
	serveDone        chan struct{}
	tlsServeDone     chan struct{}

	logger *steno.Logger
}

func NewRouter(cfg *config.Config, p proxy.Proxy, mbusClient yagnats.NATSConn, r *registry.RouteRegistry, v varz.Varz,
	logCounter *vcap.LogCounter) (*Router, error) {

	var host string
	if cfg.Status.Port != 0 {
		host = fmt.Sprintf("%s:%d", cfg.Ip, cfg.Status.Port)
	}

	varz := &vcap.Varz{
		UniqueVarz: v,
		GenericVarz: vcap.GenericVarz{
			Type:        "Router",
			Index:       cfg.Index,
			Host:        host,
			Credentials: []string{cfg.Status.User, cfg.Status.Pass},
			LogCounts:   logCounter,
		},
	}

	healthz := &vcap.Healthz{}

	component := &vcap.VcapComponent{
		Config:  cfg,
		Varz:    varz,
		Healthz: healthz,
		InfoRoutes: map[string]json.Marshaler{
			"/routes": r,
		},
		Logger: steno.NewLogger("common.logger"),
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
		logger:       steno.NewLogger("router"),
	}

	if err := router.component.Start(); err != nil {
		return nil, err
	}

	return router, nil
}

func (r *Router) Run() <-chan error {
	r.registry.StartPruningCycle()

	r.RegisterComponent()

	// Subscribe register/unregister router
	r.SubscribeRegister()
	r.HandleGreetings()
	r.SubscribeUnregister()

	// Kickstart sending start messages
	r.SendStartMessage()

	r.mbusClient.AddReconnectedCB(func(conn *nats.Conn) {
		r.logger.Infof("Reconnecting to NATS server %s...", conn.Opts.Url)
		r.SendStartMessage()
	})

	// Schedule flushing active app's app_id
	r.ScheduleFlushApps()

	// Wait for one start message send interval, such that the router's registry
	// can be populated before serving requests.
	if r.config.StartResponseDelayInterval != 0 {
		r.logger.Infof("Waiting %s before listening...", r.config.StartResponseDelayInterval)
		time.Sleep(r.config.StartResponseDelayInterval)
	}

	server := &http.Server{
		Handler:   dropsonde.InstrumentedHandler(r.proxy),
		ConnState: r.HandleConnState,
	}

	errChan := make(chan error, 2)

	err := r.serveHTTP(server, errChan)
	if err != nil {
		errChan <- err
		return errChan
	}
	err = r.serveHTTPS(server, errChan)
	if err != nil {
		errChan <- err
		return errChan
	}

	return errChan
}

func (r *Router) serveHTTPS(server *http.Server, errChan chan error) error {
	if r.config.EnableSSL {
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{r.config.SSLCertificate},
			CipherSuites: r.config.CipherSuites,
		}

		tlsListener, err := tls.Listen("tcp", fmt.Sprintf(":%d", r.config.SSLPort), tlsConfig)
		if err != nil {
			r.logger.Fatalf("tls.Listen: %s", err)
			return err
		}

		r.tlsListener = tlsListener
		r.logger.Infof("Listening on %s", tlsListener.Addr())

		go func() {
			err := server.Serve(tlsListener)
			errChan <- err
			close(r.tlsServeDone)
		}()
	}
	return nil
}

func (r *Router) serveHTTP(server *http.Server, errChan chan error) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", r.config.Port))
	if err != nil {
		r.logger.Fatalf("net.Listen: %s", err)
		return err
	}

	r.listener = listener
	r.logger.Infof("Listening on %s", listener.Addr())

	go func() {
		err := server.Serve(listener)
		errChan <- err
		close(r.serveDone)
	}()
	return nil
}

func (r *Router) Drain(drainTimeout time.Duration) error {
	r.stopListening()

	drained := make(chan struct{})
	r.connLock.Lock()

	r.logger.Infof("Draining with %d outstanding active connections", len(r.activeConns))
	r.logger.Infof("Draining with %d outstanding idle connections", len(r.idleConns))
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
		r.logger.Warn("router.drain.timed-out")
		return DrainTimeout
	}
	return nil
}

func (r *Router) Stop() {
	r.stopListening()

	r.connLock.Lock()
	r.closeIdleConns()
	r.connLock.Unlock()

	r.component.Stop()
}

// connLock must be locked
func (r *Router) closeIdleConns() {
	r.closeConnections = true

	for conn, _ := range r.idleConns {
		conn.Close()
	}
}

func (r *Router) stopListening() {
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
		r.logger.Debugf("Got router.register: %v", registryMessage)

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
		r.logger.Debugf("Got router.unregister: %v", registryMessage)

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
			r.logger.Warnf("Received message with empty reply on subject %s", msg.Subject)
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

		conn.SetDeadline(time.Time{})
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
		r.logger.Warnf("flushApps: Error marshalling JSON: %s", err)
		return
	}

	b := bytes.Buffer{}
	w := zlib.NewWriter(&b)
	w.Write(y)
	w.Close()

	z := b.Bytes()

	r.logger.Debugf("Active apps: %d, message size: %d", len(x), len(z))

	r.mbusClient.Publish("router.active_apps", z)
}

func (r *Router) greetMessage() ([]byte, error) {
	host, err := localip.LocalIP()
	if err != nil {
		return nil, err
	}

	d := vcap.RouterStart{
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
			r.logger.Warnd(map[string]interface{}{"payload": string(payload)}, logMessage)
			return
		}

		logMessage := fmt.Sprintf("%s: Received message", subject)
		r.logger.Debugd(map[string]interface{}{"message": msg}, logMessage)

		if !msg.ValidateMessage() {
			logMessage := fmt.Sprintf("%s: Unable to validate message. route_service_url must be https", subject)
			r.logger.Warnd(map[string]interface{}{"message": msg}, logMessage)
			return
		}

		successCallback(&msg)
	}

	_, err := r.mbusClient.Subscribe(subject, callback)
	if err != nil {
		r.logger.Errorf("Error subscribing to %s: %s", subject, err)
	}
}
