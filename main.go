package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"runtime"
	"syscall"
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/debugserver"
	mr "code.cloudfoundry.org/go-metric-registry"
	"code.cloudfoundry.org/tlsconfig"
	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/metric_sender"
	"github.com/cloudfoundry/dropsonde/metricbatcher"
	"github.com/nats-io/nats.go"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/sigmon"

	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/common/health"
	"code.cloudfoundry.org/gorouter/common/schema"
	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/errorwriter"
	grlog "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/mbus"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/metrics/monitor"
	"code.cloudfoundry.org/gorouter/proxy"
	rregistry "code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route_fetcher"
	"code.cloudfoundry.org/gorouter/router"
	"code.cloudfoundry.org/gorouter/routeservice"
	rvarz "code.cloudfoundry.org/gorouter/varz"
	routing_api "code.cloudfoundry.org/routing-api"
	"code.cloudfoundry.org/routing-api/uaaclient"
)

var (
	configFile string
	h          *health.Health
)

func main() {
	flag.StringVar(&configFile, "c", "", "Configuration File")
	flag.Parse()

	prefix := "gorouter.stdout"
	coreLogger := grlog.CreateLogger()
	grlog.SetLoggingLevel("INFO")

	c, err := config.DefaultConfig()
	if err != nil {
		grlog.Fatal(coreLogger, "Error loading config", grlog.ErrAttr(err))
	}

	if configFile != "" {
		c, err = config.InitConfigFromFile(configFile)
		if err != nil {
			grlog.Fatal(coreLogger, "Error loading config:", grlog.ErrAttr(err))
		}
	}
	logCounter := schema.NewLogCounter()

	if c.Logging.Syslog != "" {
		prefix = c.Logging.Syslog
	}

	grlog.SetLoggingLevel(c.Logging.Level)
	grlog.SetTimeEncoder(c.Logging.Format.Timestamp)
	logger := grlog.CreateLoggerWithSource(prefix, "")
	logger.Info("starting")
	logger.Debug("local-az-set", slog.String("AvailabilityZone", c.Zone))

	var ew errorwriter.ErrorWriter
	if c.HTMLErrorTemplateFile != "" {
		ew, err = errorwriter.NewHTMLErrorWriterFromFile(c.HTMLErrorTemplateFile)
		if err != nil {
			grlog.Fatal(logger, "new-html-error-template-from-file", grlog.ErrAttr(err))
		}
	} else {
		ew = errorwriter.NewPlaintextErrorWriter()
	}

	err = dropsonde.Initialize(c.Logging.MetronAddress, c.Logging.JobName)
	if err != nil {
		grlog.Fatal(logger, "dropsonde-initialize-error", grlog.ErrAttr(err))
	}

	logger.Info("retrieved-isolation-segments",
		slog.Any("isolation_segments", c.IsolationSegments),
		slog.String("routing_table_sharding_mode", c.RoutingTableShardingMode),
	)

	// setup number of procs
	if c.GoMaxProcs != 0 {
		runtime.GOMAXPROCS(c.GoMaxProcs)
	}

	if c.DebugAddr != "" {
		_, err = debugserver.Run(c.DebugAddr, *grlog.Conf)
		if err != nil {
			logger.Error("failed-to-start-debug-server", grlog.ErrAttr(err))
		}
	}

	logger.Info("setting-up-nats-connection")
	natsReconnected := make(chan mbus.Signal)
	natsClient := mbus.Connect(c, natsReconnected, grlog.CreateLoggerWithSource(prefix, "nats"))

	var routingAPIClient routing_api.Client

	if c.RoutingApiEnabled() {
		logger.Info("setting-up-routing-api")

		routingAPIClient, err = setupRoutingAPIClient(logger, c)
		if err != nil {
			grlog.Fatal(logger, "routing-api-connection-failed", grlog.ErrAttr(err))
		}

	}

	sender := metric_sender.NewMetricSender(dropsonde.AutowiredEmitter())

	metricsReporter := initializeMetrics(sender, c, grlog.CreateLoggerWithSource(prefix, "metricsreporter"))
	fdMonitor := initializeFDMonitor(sender, grlog.CreateLoggerWithSource(prefix, "FileDescriptor"))
	registry := rregistry.NewRouteRegistry(grlog.CreateLoggerWithSource(prefix, "registry"), c, metricsReporter)
	if c.SuspendPruningIfNatsUnavailable {
		registry.SuspendPruning(func() bool { return !(natsClient.Status() == nats.CONNECTED) })
	}

	varz := rvarz.NewVarz(registry)
	compositeReporter := &metrics.CompositeReporter{VarzReporter: varz, ProxyReporter: metricsReporter}

	accessLogger, err := accesslog.CreateRunningAccessLogger(
		grlog.CreateLoggerWithSource(prefix, "access-grlog"),
		accesslog.NewLogSender(c, dropsonde.AutowiredEmitter(), logger),
		c,
	)
	if err != nil {
		grlog.Fatal(logger, "error-creating-access-logger", grlog.ErrAttr(err))
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
		grlog.CreateLoggerWithSource(prefix, "proxy"),
		c.RouteServiceEnabled,
		c.RouteServicesHairpinning,
		c.RouteServicesHairpinningAllowlist,
		c.RouteServiceTimeout,
		crypto,
		cryptoPrev,
		c.RouteServiceRecommendHttps,
		c.RouteServiceConfig.StrictSignatureValidation,
	)

	// These TLS configs are just templates. If you add other keys you will
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
		grlog.Fatal(logger, "new-route-services-server", grlog.ErrAttr(err))
	}

	var metricsRegistry *mr.Registry
	if c.Prometheus.Port != 0 {
		metricsRegistry = mr.NewRegistry(log.Default(),
			mr.WithTLSServer(int(c.Prometheus.Port), c.Prometheus.CertPath, c.Prometheus.KeyPath, c.Prometheus.CAPath))
	}

	h = &health.Health{}
	proxyHandler := proxy.NewProxy(
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
		grlog.CreateLoggerWithSource(prefix, "router"),
		c,
		proxyHandler,
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
		grlog.Fatal(logger, "initialize-router-error", grlog.ErrAttr(err))
	}

	members := grouper.Members{}

	if c.RoutingApiEnabled() {
		routeFetcher := setupRouteFetcher(grlog.CreateLoggerWithSource(prefix, "route-fetcher"), c, registry, routingAPIClient)
		members = append(members, grouper.Member{Name: "router-fetcher", Runner: routeFetcher})
	}

	subscriber := mbus.NewSubscriber(natsClient, registry, c, natsReconnected, grlog.CreateLoggerWithSource(prefix, "subscriber"))
	natsMonitor := initializeNATSMonitor(subscriber, sender, grlog.CreateLoggerWithSource(prefix, "NATSMonitor"))

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
		grlog.Fatal(logger, "gorouter.exited-with-failure", grlog.ErrAttr(err))
	}

	os.Exit(0)
}

func initializeFDMonitor(sender *metric_sender.MetricSender, logger *slog.Logger) *monitor.FileDescriptor {
	pid := os.Getpid()
	path := fmt.Sprintf("/proc/%d/fd", pid)
	ticker := time.NewTicker(time.Second * 5)
	return monitor.NewFileDescriptor(path, ticker, sender, logger)
}

func initializeNATSMonitor(subscriber *mbus.Subscriber, sender *metric_sender.MetricSender, logger *slog.Logger) *monitor.NATSMonitor {
	ticker := time.NewTicker(time.Second * 5)
	return &monitor.NATSMonitor{
		Subscriber: subscriber,
		Sender:     sender,
		TickChan:   ticker.C,
		Logger:     logger,
	}
}

func initializeMetrics(sender *metric_sender.MetricSender, c *config.Config, logger *slog.Logger) *metrics.MetricsReporter {
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

	return &metrics.MetricsReporter{Sender: sender, Batcher: batcher, PerRequestMetricsReporting: c.PerRequestMetricsReporting, Logger: logger}
}

func createCrypto(logger *slog.Logger, secret string) *secure.AesGCM {
	// generate secure encryption key using key derivation function (pbkdf2)
	secretPbkdf2 := secure.NewPbkdf2([]byte(secret), 16)
	crypto, err := secure.NewAesGCM(secretPbkdf2)
	if err != nil {
		grlog.Fatal(logger, "error-creating-route-service-crypto", grlog.ErrAttr(err))
	}
	return crypto
}

func setupRoutingAPIClient(logger *slog.Logger, c *config.Config) (routing_api.Client, error) {
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
	clockInstance := clock.NewClock()

	uaaConfig := uaaclient.Config{
		Port:              c.OAuth.Port,
		SkipSSLValidation: c.OAuth.SkipSSLValidation,
		ClientName:        c.OAuth.ClientName,
		ClientSecret:      c.OAuth.ClientSecret,
		CACerts:           c.OAuth.CACerts,
		TokenEndpoint:     c.OAuth.TokenEndpoint,
	}

	uaaTokenFetcher, err := uaaclient.NewTokenFetcher(c.RoutingApi.AuthDisabled, uaaConfig, clockInstance, uint(c.TokenFetcherMaxRetries), c.TokenFetcherRetryInterval, c.TokenFetcherExpirationBufferTimeInSeconds, grlog.NewLagerAdapter(c.Logging.Syslog))
	if err != nil {
		grlog.Fatal(logger, "initialize-uaa-client", grlog.ErrAttr(err))
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

func setupRouteFetcher(logger *slog.Logger, c *config.Config, registry rregistry.Registry, routingAPIClient routing_api.Client) *route_fetcher.RouteFetcher {
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
	uaaTokenFetcher, err := uaaclient.NewTokenFetcher(c.RoutingApi.AuthDisabled, uaaConfig, clock, uint(c.TokenFetcherMaxRetries), c.TokenFetcherRetryInterval, c.TokenFetcherExpirationBufferTimeInSeconds, grlog.NewLagerAdapter(c.Logging.Syslog))
	if err != nil {
		grlog.Fatal(logger, "initialize-uaa-client", grlog.ErrAttr(err))
	}

	_, err = uaaTokenFetcher.FetchToken(context.Background(), true)
	if err != nil {
		grlog.Fatal(logger, "unable-to-fetch-token", grlog.ErrAttr(err))
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
