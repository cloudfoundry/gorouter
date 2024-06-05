package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"syscall"
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/debugserver"
	mr "code.cloudfoundry.org/go-metric-registry"
	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/common/health"
	"code.cloudfoundry.org/gorouter/common/schema"
	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/errorwriter"
	goRouterLogger "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/mbus"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/metrics/monitor"
	"code.cloudfoundry.org/gorouter/proxy"
	rregistry "code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route_fetcher"
	"code.cloudfoundry.org/gorouter/router"
	"code.cloudfoundry.org/gorouter/routeservice"
	rvarz "code.cloudfoundry.org/gorouter/varz"
	"code.cloudfoundry.org/lager/v3"
	routing_api "code.cloudfoundry.org/routing-api"
	"code.cloudfoundry.org/routing-api/uaaclient"
	"code.cloudfoundry.org/tlsconfig"
	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/metric_sender"
	"github.com/cloudfoundry/dropsonde/metricbatcher"
	"github.com/nats-io/nats.go"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/sigmon"
	"github.com/uber-go/zap"
)

var (
	configFile string
	h          *health.Health
)

func main() {
	flag.StringVar(&configFile, "c", "", "Configuration File")
	flag.Parse()

	prefix := "gorouter.stdout"
	tmpLogger, _ := createLogger(prefix, "INFO", "unix-epoch")

	c, err := config.DefaultConfig()
	if err != nil {
		tmpLogger.Fatal("Error loading config:", zap.Error(err))
	}

	if configFile != "" {
		c, err = config.InitConfigFromFile(configFile)
		if err != nil {
			tmpLogger.Fatal("Error loading config:", zap.Error(err))
		}
	}

	logCounter := schema.NewLogCounter()

	if c.Logging.Syslog != "" {
		prefix = c.Logging.Syslog
	}
	logger, minLagerLogLevel := createLogger(prefix, c.Logging.Level, c.Logging.Format.Timestamp)
	logger.Info("starting")
	logger.Debug("local-az-set", zap.String("AvailabilityZone", c.Zone))

	var ew errorwriter.ErrorWriter
	if c.HTMLErrorTemplateFile != "" {
		ew, err = errorwriter.NewHTMLErrorWriterFromFile(c.HTMLErrorTemplateFile)
		if err != nil {
			logger.Fatal("new-html-error-template-from-file", zap.Error(err))
		}
	} else {
		ew = errorwriter.NewPlaintextErrorWriter()
	}

	err = dropsonde.Initialize(c.Logging.MetronAddress, c.Logging.JobName)
	if err != nil {
		logger.Fatal("dropsonde-initialize-error", zap.Error(err))
	}

	logger.Info("retrieved-isolation-segments",
		zap.Object("isolation_segments", c.IsolationSegments),
		zap.Object("routing_table_sharding_mode", c.RoutingTableShardingMode),
	)

	// setup number of procs
	if c.GoMaxProcs != 0 {
		runtime.GOMAXPROCS(c.GoMaxProcs)
	}

	if c.DebugAddr != "" {
		reconfigurableSink := lager.NewReconfigurableSink(lager.NewWriterSink(os.Stdout, lager.DEBUG), minLagerLogLevel)
		debugserver.Run(c.DebugAddr, reconfigurableSink)
	}

	logger.Info("setting-up-nats-connection")
	natsReconnected := make(chan mbus.Signal)
	natsClient := mbus.Connect(c, natsReconnected, logger.Session("nats"))

	var routingAPIClient routing_api.Client

	if c.RoutingApiEnabled() {
		logger.Info("setting-up-routing-api")

		routingAPIClient, err = setupRoutingAPIClient(logger, c)
		if err != nil {
			logger.Fatal("routing-api-connection-failed", zap.Error(err))
		}

	}

	sender := metric_sender.NewMetricSender(dropsonde.AutowiredEmitter())

	metricsReporter := initializeMetrics(sender, c)
	fdMonitor := initializeFDMonitor(sender, logger)
	registry := rregistry.NewRouteRegistry(logger.Session("registry"), c, metricsReporter)
	if c.SuspendPruningIfNatsUnavailable {
		registry.SuspendPruning(func() bool { return !(natsClient.Status() == nats.CONNECTED) })
	}

	varz := rvarz.NewVarz(registry)
	compositeReporter := &metrics.CompositeReporter{VarzReporter: varz, ProxyReporter: metricsReporter}

	accessLogger, err := accesslog.CreateRunningAccessLogger(
		logger.Session("access-log"),
		accesslog.NewLogSender(c, dropsonde.AutowiredEmitter(), logger),
		c,
	)
	if err != nil {
		logger.Fatal("error-creating-access-logger", zap.Error(err))
	}

	var crypto secure.Crypto
	var cryptoPrev secure.Crypto
	if c.RouteServiceEnabled {
		crypto = createCrypto(logger, c.RouteServiceSecret)
		if c.RouteServiceSecretPrev != "" {
			cryptoPrev = createCrypto(logger, c.RouteServiceSecretPrev)
		}
	}

	routeServiceConfig := routeservice.NewRouteServiceConfig(
		logger.Session("proxy"),
		c.RouteServiceEnabled,
		c.RouteServicesHairpinning,
		c.RouteServicesHairpinningAllowlist,
		c.RouteServiceTimeout,
		crypto,
		cryptoPrev,
		c.RouteServiceRecommendHttps,
		c.RouteServiceConfig.StrictSignatureValidation,
	)

	// These TLS configs are just tempaltes. If you add other keys you will
	// also need to edit proxy/utils/tls_config.go
	backendTLSConfig := &tls.Config{
		CipherSuites: c.CipherSuites,
		RootCAs:      c.CAPool,
		Certificates: []tls.Certificate{c.Backends.ClientAuthCertificate},
	}

	routeServiceTLSConfig := &tls.Config{
		CipherSuites:       c.CipherSuites,
		InsecureSkipVerify: c.SkipSSLValidation,
		RootCAs:            c.CAPool,
		Certificates:       []tls.Certificate{c.RouteServiceConfig.ClientAuthCertificate},
		MinVersion:         c.MinTLSVersion,
		MaxVersion:         c.MaxTLSVersion,
	}

	rss, err := router.NewRouteServicesServer(c)
	if err != nil {
		logger.Fatal("new-route-services-server", zap.Error(err))
	}

	var metricsRegistry *mr.Registry
	if c.Prometheus.Port != 0 {
		metricsRegistry = mr.NewRegistry(log.Default(),
			mr.WithTLSServer(int(c.Prometheus.Port), c.Prometheus.CertPath, c.Prometheus.KeyPath, c.Prometheus.CAPath))
	}

	h = &health.Health{}
	proxy := proxy.NewProxy(
		logger,
		accessLogger,
		metricsRegistry,
		ew,
		c,
		registry,
		compositeReporter,
		routeServiceConfig,
		backendTLSConfig,
		routeServiceTLSConfig,
		h,
		rss.GetRoundTripper(),
	)

	var errorChannel chan error = nil

	goRouter, err := router.NewRouter(
		logger.Session("router"),
		c,
		proxy,
		natsClient,
		registry,
		varz,
		h,
		logCounter,
		errorChannel,
		rss,
	)

	h.OnDegrade = goRouter.DrainAndStop

	if err != nil {
		logger.Fatal("initialize-router-error", zap.Error(err))
	}

	members := grouper.Members{}

	if c.RoutingApiEnabled() {
		routeFetcher := setupRouteFetcher(logger.Session("route-fetcher"), c, registry, routingAPIClient)
		members = append(members, grouper.Member{Name: "router-fetcher", Runner: routeFetcher})
	}

	subscriber := mbus.NewSubscriber(natsClient, registry, c, natsReconnected, logger.Session("subscriber"))
	natsMonitor := initializeNATSMonitor(subscriber, sender, logger)

	members = append(members, grouper.Member{Name: "fdMonitor", Runner: fdMonitor})
	members = append(members, grouper.Member{Name: "subscriber", Runner: subscriber})
	members = append(members, grouper.Member{Name: "natsMonitor", Runner: natsMonitor})
	members = append(members, grouper.Member{Name: "router", Runner: goRouter})

	group := grouper.NewOrdered(os.Interrupt, members)

	monitor := ifrit.Invoke(sigmon.New(group, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1))

	go func() {
		time.Sleep(c.RouteLatencyMetricMuzzleDuration) // this way we avoid reporting metrics for pre-existing routes
		metricsReporter.UnmuzzleRouteRegistrationLatency()
	}()

	<-monitor.Ready()
	h.SetHealth(health.Healthy)

	err = <-monitor.Wait()
	if err != nil {
		logger.Error("gorouter.exited-with-failure", zap.Error(err))
		os.Exit(1)
	}

	os.Exit(0)
}

func initializeFDMonitor(sender *metric_sender.MetricSender, logger goRouterLogger.Logger) *monitor.FileDescriptor {
	pid := os.Getpid()
	path := fmt.Sprintf("/proc/%d/fd", pid)
	ticker := time.NewTicker(time.Second * 5)
	return monitor.NewFileDescriptor(path, ticker, sender, logger.Session("FileDescriptor"))
}

func initializeNATSMonitor(subscriber *mbus.Subscriber, sender *metric_sender.MetricSender, logger goRouterLogger.Logger) *monitor.NATSMonitor {
	ticker := time.NewTicker(time.Second * 5)
	return &monitor.NATSMonitor{
		Subscriber: subscriber,
		Sender:     sender,
		TickChan:   ticker.C,
		Logger:     logger.Session("NATSMonitor"),
	}
}

func initializeMetrics(sender *metric_sender.MetricSender, c *config.Config) *metrics.MetricsReporter {
	// 5 sec is dropsonde default batching interval
	batcher := metricbatcher.New(sender, 5*time.Second)
	batcher.AddConsistentlyEmittedMetrics("bad_gateways",
		"backend_exhausted_conns",
		"backend_invalid_id",
		"backend_invalid_tls_cert",
		"backend_tls_handshake_failed",
		"rejected_requests",
		"total_requests",
		"responses",
		"responses.2xx",
		"responses.3xx",
		"responses.4xx",
		"responses.5xx",
		"responses.xxx",
		"routed_app_requests",
		"routes_pruned",
		"websocket_failures",
		"websocket_upgrades",
	)

	return &metrics.MetricsReporter{Sender: sender, Batcher: batcher, PerRequestMetricsReporting: c.PerRequestMetricsReporting}
}

func createCrypto(logger goRouterLogger.Logger, secret string) *secure.AesGCM {
	// generate secure encryption key using key derivation function (pbkdf2)
	secretPbkdf2 := secure.NewPbkdf2([]byte(secret), 16)
	crypto, err := secure.NewAesGCM(secretPbkdf2)
	if err != nil {
		logger.Fatal("error-creating-route-service-crypto", zap.Error(err))
	}
	return crypto
}

func setupRoutingAPIClient(logger goRouterLogger.Logger, c *config.Config) (routing_api.Client, error) {
	routingAPIURI := fmt.Sprintf("%s:%d", c.RoutingApi.Uri, c.RoutingApi.Port)

	tlsConfig, err := tlsconfig.Build(
		tlsconfig.WithInternalServiceDefaults(),
		tlsconfig.WithIdentity(c.RoutingApi.ClientAuthCertificate),
	).Client(
		tlsconfig.WithAuthority(c.RoutingApi.CAPool),
	)
	if err != nil {
		return nil, err
	}

	client := routing_api.NewClientWithTLSConfig(routingAPIURI, tlsConfig)

	logger.Debug("fetching-token")
	clock := clock.NewClock()

	uaaConfig := uaaclient.Config{
		Port:              c.OAuth.Port,
		SkipSSLValidation: c.OAuth.SkipSSLValidation,
		ClientName:        c.OAuth.ClientName,
		ClientSecret:      c.OAuth.ClientSecret,
		CACerts:           c.OAuth.CACerts,
		TokenEndpoint:     c.OAuth.TokenEndpoint,
	}

	uaaTokenFetcher, err := uaaclient.NewTokenFetcher(c.RoutingApi.AuthDisabled, uaaConfig, clock, uint(c.TokenFetcherMaxRetries), c.TokenFetcherRetryInterval, c.TokenFetcherExpirationBufferTimeInSeconds, goRouterLogger.NewLagerAdapter(logger))
	if err != nil {
		logger.Fatal("initialize-uaa-client", zap.Error(err))
	}

	if !c.RoutingApi.AuthDisabled {
		token, err := uaaTokenFetcher.FetchToken(context.Background(), true)
		if err != nil {
			return nil, fmt.Errorf("unable-to-fetch-token: %s", err.Error())
		}
		if token.AccessToken == "" {
			return nil, fmt.Errorf("empty token fetched")
		}
		client.SetToken(token.AccessToken)
	}
	// Test connectivity
	if _, err := client.Routes(); err != nil {
		return nil, err
	}

	return client, nil
}

func setupRouteFetcher(logger goRouterLogger.Logger, c *config.Config, registry rregistry.Registry, routingAPIClient routing_api.Client) *route_fetcher.RouteFetcher {
	cl := clock.NewClock()

	uaaConfig := uaaclient.Config{
		Port:              c.OAuth.Port,
		SkipSSLValidation: c.OAuth.SkipSSLValidation,
		ClientName:        c.OAuth.ClientName,
		ClientSecret:      c.OAuth.ClientSecret,
		CACerts:           c.OAuth.CACerts,
		TokenEndpoint:     c.OAuth.TokenEndpoint,
	}
	clock := clock.NewClock()
	uaaTokenFetcher, err := uaaclient.NewTokenFetcher(c.RoutingApi.AuthDisabled, uaaConfig, clock, uint(c.TokenFetcherMaxRetries), c.TokenFetcherRetryInterval, c.TokenFetcherExpirationBufferTimeInSeconds, goRouterLogger.NewLagerAdapter(logger))
	if err != nil {
		logger.Fatal("initialize-uaa-client", zap.Error(err))
	}

	_, err = uaaTokenFetcher.FetchToken(context.Background(), true)
	if err != nil {
		logger.Fatal("unable-to-fetch-token", zap.Error(err))
	}

	subscriptionRetryInterval := 1 * time.Second

	routeFetcher := route_fetcher.NewRouteFetcher(
		logger,
		uaaTokenFetcher,
		registry,
		c,
		routingAPIClient,
		subscriptionRetryInterval,
		cl,
	)
	return routeFetcher
}

func createLogger(component string, level string, timestampFormat string) (goRouterLogger.Logger, lager.LogLevel) {
	var logLevel zap.Level
	logLevel.UnmarshalText([]byte(level))

	var minLagerLogLevel lager.LogLevel
	switch minLagerLogLevel {
	case lager.DEBUG:
		minLagerLogLevel = lager.DEBUG
	case lager.INFO:
		minLagerLogLevel = lager.INFO
	case lager.ERROR:
		minLagerLogLevel = lager.ERROR
	case lager.FATAL:
		minLagerLogLevel = lager.FATAL
	default:
		panic(fmt.Errorf("unknown log level: %s", level))
	}

	lggr := goRouterLogger.NewLogger(component, timestampFormat, logLevel, zap.Output(os.Stdout))
	return lggr, minLagerLogLevel
}
