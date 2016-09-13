package main

import (
	"crypto/tls"
	"errors"
	"net/url"

	"code.cloudfoundry.org/cflager"
	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/debugserver"
	"code.cloudfoundry.org/gorouter/access_log"
	"code.cloudfoundry.org/gorouter/common/schema"
	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/metrics/reporter"
	"code.cloudfoundry.org/gorouter/proxy"
	rregistry "code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route_fetcher"
	"code.cloudfoundry.org/gorouter/router"
	rvarz "code.cloudfoundry.org/gorouter/varz"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/routing-api"
	uaa_client "code.cloudfoundry.org/uaa-go-client"
	uaa_config "code.cloudfoundry.org/uaa-go-client/config"
	"github.com/cloudfoundry/dropsonde"
	"github.com/nats-io/nats"

	"flag"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"

	"code.cloudfoundry.org/gorouter/metrics"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/sigmon"
)

var configFile string

const (
	DEBUG = "debug"
	INFO  = "info"
	ERROR = "error"
	FATAL = "fatal"
)

func main() {
	flag.StringVar(&configFile, "c", "", "Configuration File")
	cflager.AddFlags(flag.CommandLine)
	flag.Parse()

	c := config.DefaultConfig()
	logCounter := schema.NewLogCounter()

	if configFile != "" {
		c = config.InitConfigFromFile(configFile)
	}

	prefix := "gorouter.stdout"
	if c.Logging.Syslog != "" {
		prefix = c.Logging.Syslog
	}
	logger, reconfigurableSink := cflager.New(prefix)
	InitLoggerFromConfig(logger, c, logCounter)

	logger.Info("starting")

	err := dropsonde.Initialize(c.Logging.MetronAddress, c.Logging.JobName)
	if err != nil {
		logger.Fatal("dropsonde-initialize-error", err)
	}

	// setup number of procs
	if c.GoMaxProcs != 0 {
		runtime.GOMAXPROCS(c.GoMaxProcs)
	}

	if c.DebugAddr != "" {
		debugserver.Run(c.DebugAddr, reconfigurableSink)
	}

	logger.Info("setting-up-nats-connection")
	natsClient := connectToNatsServer(logger.Session("nats"), c)
	natsHost := ""
	natsUrl, err := url.Parse(natsClient.ConnectedUrl())
	if err == nil {
		natsHost = natsUrl.Host
	}

	logger.Info("Successfully-connected-to-nats", lager.Data{"host": natsHost})

	metricsReporter := metrics.NewMetricsReporter()
	registry := rregistry.NewRouteRegistry(logger.Session("registry"), c, metricsReporter)
	if c.SuspendPruningIfNatsUnavailable {
		registry.SuspendPruning(func() bool { return !(natsClient.Status() == nats.CONNECTED) })
	}

	varz := rvarz.NewVarz(registry)
	compositeReporter := metrics.NewCompositeReporter(varz, metricsReporter)

	accessLogger, err := access_log.CreateRunningAccessLogger(logger.Session("access-log"), c)
	if err != nil {
		logger.Fatal("error-creating-access-logger", err)
	}

	var crypto secure.Crypto
	var cryptoPrev secure.Crypto
	if c.RouteServiceEnabled {
		crypto = createCrypto(logger, c.RouteServiceSecret)
		if c.RouteServiceSecretPrev != "" {
			cryptoPrev = createCrypto(logger, c.RouteServiceSecretPrev)
		}
	}

	proxy := buildProxy(logger.Session("proxy"), c, registry, accessLogger, compositeReporter, crypto, cryptoPrev)

	router, err := router.NewRouter(logger.Session("router"), c, proxy, natsClient, registry, varz, logCounter, nil)
	if err != nil {
		logger.Fatal("initialize-router-error", err)
	}

	members := grouper.Members{
		{"router", router},
	}
	if c.RoutingApiEnabled() {
		logger.Info("setting-up-routing-api")
		routeFetcher := setupRouteFetcher(logger.Session("route-fetcher"), c, registry)

		// check connectivity to routing api
		err = routeFetcher.FetchRoutes()
		if err != nil {
			logger.Fatal("routing-api-connection-failed", err)
		}
		members = append(members, grouper.Member{Name: "router-fetcher", Runner: routeFetcher})
	}

	group := grouper.NewOrdered(os.Interrupt, members)

	monitor := ifrit.Invoke(sigmon.New(group, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1))

	err = <-monitor.Wait()
	if err != nil {
		logger.Error("gorouter.exited-with-failure", err)
		os.Exit(1)
	}

	os.Exit(0)
}

func createCrypto(logger lager.Logger, secret string) *secure.AesGCM {
	// generate secure encryption key using key derivation function (pbkdf2)
	secretPbkdf2 := secure.NewPbkdf2([]byte(secret), 16)
	crypto, err := secure.NewAesGCM(secretPbkdf2)
	if err != nil {
		logger.Fatal("error-creating-route-service-crypto", err)
	}
	return crypto
}

func buildProxy(logger lager.Logger, c *config.Config, registry rregistry.RegistryInterface, accessLogger access_log.AccessLogger, reporter reporter.ProxyReporter, crypto secure.Crypto, cryptoPrev secure.Crypto) proxy.Proxy {
	args := proxy.ProxyArgs{
		Logger:          logger,
		EndpointTimeout: c.EndpointTimeout,
		Ip:              c.Ip,
		TraceKey:        c.TraceKey,
		Registry:        registry,
		Reporter:        reporter,
		AccessLogger:    accessLogger,
		SecureCookies:   c.SecureCookies,
		TLSConfig: &tls.Config{
			CipherSuites:       c.CipherSuites,
			InsecureSkipVerify: c.SkipSSLValidation,
		},
		RouteServiceEnabled:        c.RouteServiceEnabled,
		RouteServiceTimeout:        c.RouteServiceTimeout,
		RouteServiceRecommendHttps: c.RouteServiceRecommendHttps,
		Crypto:               crypto,
		CryptoPrev:           cryptoPrev,
		ExtraHeadersToLog:    c.ExtraHeadersToLog,
		HealthCheckUserAgent: c.HealthCheckUserAgent,
		EnableZipkin:         c.Tracing.EnableZipkin,
		DefaultLoadBalance:   c.LoadBalance,
	}
	return proxy.NewProxy(args)
}

func setupRouteFetcher(logger lager.Logger, c *config.Config, registry rregistry.RegistryInterface) *route_fetcher.RouteFetcher {
	clock := clock.NewClock()

	uaaClient := newUaaClient(logger, clock, c)

	_, err := uaaClient.FetchToken(true)
	if err != nil {
		logger.Fatal("unable-to-fetch-token", err)
	}

	routingApiUri := fmt.Sprintf("%s:%d", c.RoutingApi.Uri, c.RoutingApi.Port)
	routingApiClient := routing_api.NewClient(routingApiUri, false)

	routeFetcher := route_fetcher.NewRouteFetcher(logger, uaaClient, registry, c, routingApiClient, 1, clock)
	return routeFetcher
}

func newUaaClient(logger lager.Logger, clock clock.Clock, c *config.Config) uaa_client.Client {
	if c.RoutingApi.AuthDisabled {
		logger.Info("using-noop-token-fetcher")
		return uaa_client.NewNoOpUaaClient()
	}

	if c.OAuth.Port == -1 {
		logger.Fatal("tls-not-enabled", errors.New("GoRouter requires TLS enabled to get OAuth token"), lager.Data{"token-endpoint": c.OAuth.TokenEndpoint, "port": c.OAuth.Port})
	}

	tokenURL := fmt.Sprintf("https://%s:%d", c.OAuth.TokenEndpoint, c.OAuth.Port)

	cfg := &uaa_config.Config{
		UaaEndpoint:           tokenURL,
		SkipVerification:      c.OAuth.SkipSSLValidation,
		ClientName:            c.OAuth.ClientName,
		ClientSecret:          c.OAuth.ClientSecret,
		CACerts:               c.OAuth.CACerts,
		MaxNumberOfRetries:    c.TokenFetcherMaxRetries,
		RetryInterval:         c.TokenFetcherRetryInterval,
		ExpirationBufferInSec: c.TokenFetcherExpirationBufferTimeInSeconds,
	}

	uaaClient, err := uaa_client.NewClient(logger, cfg, clock)
	if err != nil {
		logger.Fatal("initialize-token-fetcher-error", err)
	}
	return uaaClient
}

func connectToNatsServer(logger lager.Logger, c *config.Config) *nats.Conn {
	var natsClient *nats.Conn
	var err error

	natsServers := c.NatsServers()
	attempts := 3
	for attempts > 0 {
		options := nats.DefaultOptions
		options.Servers = natsServers
		options.PingInterval = c.NatsClientPingInterval
		options.ClosedCB = func(conn *nats.Conn) {
			logger.Fatal("nats-connection-closed", errors.New("unexpected close"), lager.Data{"last_error": conn.LastError()})
		}

		// in the case of suspending pruning, we need to ensure we retry reconnects indefinitely
		if c.SuspendPruningIfNatsUnavailable {
			options.MaxReconnect = -1
		}
		natsClient, err = options.Connect()
		if err == nil {
			break
		} else {
			attempts--
			time.Sleep(100 * time.Millisecond)
		}
	}

	if err != nil {
		logger.Fatal("nats-connection-error", err)
	}

	return natsClient
}

func InitLoggerFromConfig(logger lager.Logger, c *config.Config, logCounter *schema.LogCounter) {
	if c.Logging.File != "" {
		file, err := os.OpenFile(c.Logging.File, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
		if err != nil {
			logger.Fatal("error-opening-log-file", err, lager.Data{"file": c.Logging.File})
		}
		var logLevel lager.LogLevel
		switch c.Logging.Level {
		case DEBUG:
			logLevel = lager.DEBUG
		case INFO:
			logLevel = lager.INFO
		case ERROR:
			logLevel = lager.ERROR
		case FATAL:
			logLevel = lager.FATAL
		default:
			panic(fmt.Errorf("unknown log level: %s", c.Logging.Level))
		}
		logger.RegisterSink(lager.NewWriterSink(file, logLevel))
	}

	logger.RegisterSink(logCounter)
}
