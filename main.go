package main

import (
	"crypto/tls"
	"encoding/base64"

	"github.com/apcera/nats"
	cf_debug_server "github.com/cloudfoundry-incubator/cf-debug-server"
	"github.com/cloudfoundry-incubator/routing-api"
	token_fetcher "github.com/cloudfoundry-incubator/uaa-token-fetcher"
	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/gorouter/access_log"
	vcap "github.com/cloudfoundry/gorouter/common"
	"github.com/cloudfoundry/gorouter/common/secure"
	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/proxy"
	rregistry "github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route_fetcher"
	"github.com/cloudfoundry/gorouter/router"
	rvarz "github.com/cloudfoundry/gorouter/varz"
	steno "github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/yagnats"

	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

var configFile string

func init() {
	flag.StringVar(&configFile, "c", "", "Configuration File")

	flag.Parse()
}

func main() {
	c := config.DefaultConfig()
	logCounter := vcap.NewLogCounter()

	if configFile != "" {
		c = config.InitConfigFromFile(configFile)
	}

	InitLoggerFromConfig(c, logCounter)
	logger := steno.NewLogger("router.main")

	err := dropsonde.Initialize(c.Logging.MetronAddress, c.Logging.JobName)
	if err != nil {
		logger.Errorf("Dropsonde failed to initialize: %s", err.Error())
		os.Exit(1)
	}

	// setup number of procs
	if c.GoMaxProcs != 0 {
		runtime.GOMAXPROCS(c.GoMaxProcs)
	}

	if c.DebugAddr != "" {
		cf_debug_server.Run(c.DebugAddr)
	}

	logger.Info("Setting up NATs connection")
	natsClient := connectToNatsServer(c, logger)

	registry := rregistry.NewRouteRegistry(c, natsClient)

	logger.Info("Setting up routing_api route fetcher")
	setupRouteFetcher(c, registry)

	varz := rvarz.NewVarz(registry)

	accessLogger, err := access_log.CreateRunningAccessLogger(c)
	if err != nil {
		logger.Fatalf("Error creating access logger: %s\n", err)
	}

	routeServiceSecretDecoded, err := base64.StdEncoding.DecodeString(c.RouteServiceSecret)
	if err != nil {
		logger.Errorf("Error decoding route service secret: %s\n", err)
		os.Exit(1)
	}

	crypto, err := secure.NewAesGCM(routeServiceSecretDecoded)

	if err != nil {
		logger.Errorf("Error creating route service crypto: %s\n", err)
		os.Exit(1)
	}

	proxy := buildProxy(c, registry, accessLogger, varz, crypto)

	router, err := router.NewRouter(c, proxy, natsClient, registry, varz, logCounter)
	if err != nil {
		logger.Errorf("An error occurred: %s", err.Error())
		os.Exit(1)
	}

	errChan := router.Run()

	logger.Info("gorouter.started")

	waitOnErrOrSignal(c, logger, errChan, router)

	os.Exit(0)
}

func waitOnErrOrSignal(c *config.Config, logger *steno.Logger, errChan <-chan error, router *router.Router) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1)

	select {
	case err := <-errChan:
		if err != nil {
			logger.Errorf("Error occurred: %s", err.Error())
			os.Exit(1)
		}
	case sig := <-signals:
		go func() {
			for sig := range signals {
				logger.Infod(
					map[string]interface{}{
						"signal": sig.String(),
					},
					"gorouter.signal.ignored",
				)
			}
		}()

		if sig == syscall.SIGUSR1 {
			logger.Infod(
				map[string]interface{}{
					"timeout": (c.DrainTimeout).String(),
				},
				"gorouter.draining",
			)

			router.Drain(c.DrainTimeout)
		}

		stoppingAt := time.Now()

		logger.Info("gorouter.stopping")

		router.Stop()

		logger.Infod(
			map[string]interface{}{
				"took": time.Since(stoppingAt).String(),
			},
			"gorouter.stopped",
		)
	}
}

func buildProxy(c *config.Config, registry rregistry.RegistryInterface, accessLogger access_log.AccessLogger, varz rvarz.Varz, crypto secure.Crypto) proxy.Proxy {
	args := proxy.ProxyArgs{
		EndpointTimeout: c.EndpointTimeout,
		Ip:              c.Ip,
		TraceKey:        c.TraceKey,
		Registry:        registry,
		Reporter:        varz,
		AccessLogger:    accessLogger,
		SecureCookies:   c.SecureCookies,
		TLSConfig: &tls.Config{
			CipherSuites:       c.CipherSuites,
			InsecureSkipVerify: c.SSLSkipValidation,
		},
		RouteServiceTimeout: c.RouteServiceTimeout,
		Crypto:              crypto,
	}
	return proxy.NewProxy(args)
}

func setupRouteFetcher(c *config.Config, registry rregistry.RegistryInterface) {
	if c.RoutingApiEnabled() {
		tokenFetcher := token_fetcher.NewTokenFetcher(&c.OAuth)
		routingApiUri := fmt.Sprintf("%s:%d", c.RoutingApi.Uri, c.RoutingApi.Port)
		routingApiClient := routing_api.NewClient(routingApiUri)
		routeFetcher := route_fetcher.NewRouteFetcher(steno.NewLogger("router.route_fetcher"), tokenFetcher, registry, c, routingApiClient, 1)
		routeFetcher.StartFetchCycle()
		routeFetcher.StartEventCycle()
	}
}

func connectToNatsServer(c *config.Config, logger *steno.Logger) yagnats.NATSConn {
	var natsClient yagnats.NATSConn
	var err error

	natsServers := c.NatsServers()
	attempts := 3
	for attempts > 0 {
		natsClient, err = yagnats.Connect(natsServers)
		if err == nil {
			break
		} else {
			attempts--
			time.Sleep(100 * time.Millisecond)
		}
	}

	if err != nil {
		logger.Errorf("Error connecting to NATS: %s\n", err)
		os.Exit(1)
	}

	natsClient.AddClosedCB(func(conn *nats.Conn) {
		logger.Errorf("Close on NATS client. nats.Conn: %+v", *conn)
		os.Exit(1)
	})

	return natsClient
}

func InitLoggerFromConfig(c *config.Config, logCounter *vcap.LogCounter) {
	l, err := steno.GetLogLevel(c.Logging.Level)
	if err != nil {
		panic(err)
	}

	s := make([]steno.Sink, 0, 3)
	if c.Logging.File != "" {
		s = append(s, steno.NewFileSink(c.Logging.File))
	} else {
		s = append(s, steno.NewIOSink(os.Stdout))
	}

	if c.Logging.Syslog != "" {
		s = append(s, steno.NewSyslogSink(c.Logging.Syslog))
	}

	s = append(s, logCounter)

	stenoConfig := &steno.Config{
		Sinks: s,
		Codec: steno.NewJsonCodec(),
		Level: l,
	}

	steno.Init(stenoConfig)
}
