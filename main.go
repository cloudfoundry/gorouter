package main

import (
	"crypto/tls"

	"github.com/apcera/nats"
	cf_debug_server "github.com/cloudfoundry-incubator/cf-debug-server"
	"github.com/cloudfoundry-incubator/cf-lager"
	routing_api "github.com/cloudfoundry-incubator/routing-api"
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
	"github.com/pivotal-golang/clock"

	"flag"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/cloudfoundry/gorouter/metrics"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/sigmon"
)

var configFile string

func main() {
	flag.StringVar(&configFile, "c", "", "Configuration File")
	cf_lager.AddFlags(flag.CommandLine)
	flag.Parse()

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

	metricsReporter := metrics.NewMetricsReporter()
	registry := rregistry.NewRouteRegistry(c, natsClient, metricsReporter)

	logger.Info("Setting up routing_api route fetcher")
	setupRouteFetcher(c, registry, logger)

	varz := rvarz.NewVarz(registry)
	compositeReporter := metrics.NewCompositeReporter(varz, metricsReporter)

	accessLogger, err := access_log.CreateRunningAccessLogger(c)
	if err != nil {
		logger.Fatalf("Error creating access logger: %s\n", err)
	}

	var crypto secure.Crypto
	var cryptoPrev secure.Crypto
	if c.RouteServiceEnabled {
		crypto = createCrypto(c.RouteServiceSecret, logger)
		if c.RouteServiceSecretPrev != "" {
			cryptoPrev = createCrypto(c.RouteServiceSecretPrev, logger)
		}
	}

	proxy := buildProxy(c, registry, accessLogger, compositeReporter, crypto, cryptoPrev)

	router, err := router.NewRouter(c, proxy, natsClient, registry, varz, logCounter, nil)
	if err != nil {
		logger.Errorf("An error occurred: %s", err.Error())
		os.Exit(1)
	}

	launchRouter(c, router, logger)
}

func launchRouter(c *config.Config, router *router.Router, logger *steno.Logger) {
	members := grouper.Members{
		{"router", router},
	}

	group := grouper.NewOrdered(os.Interrupt, members)

	monitor := ifrit.Invoke(sigmon.New(group, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1))

	err := <-monitor.Wait()
	if err != nil {
		logger.Error("gorouter.exited-with-failure")
		os.Exit(1)
	}

	os.Exit(0)
}

func createCrypto(secret string, logger *steno.Logger) *secure.AesGCM {
	// generate secure encryption key using key derivation function (pbkdf2)
	secretPbkdf2 := secure.NewPbkdf2([]byte(secret), 16)
	crypto, err := secure.NewAesGCM(secretPbkdf2)
	if err != nil {
		logger.Errorf("Error creating route service crypto: %s\n", err)
		os.Exit(1)
	}
	return crypto
}

func buildProxy(c *config.Config, registry rregistry.RegistryInterface, accessLogger access_log.AccessLogger, reporter metrics.ProxyReporter, crypto secure.Crypto, cryptoPrev secure.Crypto) proxy.Proxy {
	args := proxy.ProxyArgs{
		EndpointTimeout: c.EndpointTimeout,
		Ip:              c.Ip,
		TraceKey:        c.TraceKey,
		Registry:        registry,
		Reporter:        reporter,
		AccessLogger:    accessLogger,
		SecureCookies:   c.SecureCookies,
		TLSConfig: &tls.Config{
			CipherSuites:       c.CipherSuites,
			InsecureSkipVerify: c.SSLSkipValidation,
		},
		RouteServiceEnabled: c.RouteServiceEnabled,
		RouteServiceTimeout: c.RouteServiceTimeout,
		Crypto:              crypto,
		CryptoPrev:          cryptoPrev,
		ExtraHeadersToLog:   c.ExtraHeadersToLog,
	}
	return proxy.NewProxy(args)
}

func setupRouteFetcher(c *config.Config, registry rregistry.RegistryInterface, logger *steno.Logger) {
	if c.RoutingApiEnabled() {
		tokenFetcher := newTokenFetcher(c, logger)
		routingApiUri := fmt.Sprintf("%s:%d", c.RoutingApi.Uri, c.RoutingApi.Port)
		routingApiClient := routing_api.NewClient(routingApiUri)
		routeFetcher := route_fetcher.NewRouteFetcher(steno.NewLogger("router.route_fetcher"), tokenFetcher, registry, c, routingApiClient, 1)
		routeFetcher.StartFetchCycle()
		routeFetcher.StartEventCycle()
	}
}

func newTokenFetcher(c *config.Config, logger *steno.Logger) token_fetcher.TokenFetcher {
	if c.RoutingApi.AuthDisabled {
		logger.Info("using noop token fetcher")
		return token_fetcher.NewNoOpTokenFetcher()
	}
	clock := clock.NewClock()
	tokenFetcherConfig := token_fetcher.TokenFetcherConfig{
		MaxNumberOfRetries:   c.TokenFetcherMaxRetries,
		RetryInterval:        c.TokenFetcherRetryInterval,
		ExpirationBufferTime: c.TokenFetcherExpirationBufferTimeInSeconds,
	}
	cfLogger, _ := cf_lager.New("router-configurer")
	tokenFetcher, err := token_fetcher.NewTokenFetcher(cfLogger, &c.OAuth, tokenFetcherConfig, clock)
	if err != nil {
		logger.Errorf("Error creating token fetcher: %s\n", err)
		os.Exit(1)
	}
	logger.Info("using uaa token fetcher")
	return tokenFetcher
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
