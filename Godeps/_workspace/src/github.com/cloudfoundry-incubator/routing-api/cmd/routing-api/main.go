package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/cactus/go-statsd-client/statsd"
	"github.com/cloudfoundry-incubator/cf-debug-server"
	routing_api "github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/authentication"
	"github.com/cloudfoundry-incubator/routing-api/config"
	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry-incubator/routing-api/handlers"
	"github.com/cloudfoundry-incubator/routing-api/helpers"
	"github.com/cloudfoundry-incubator/routing-api/metrics"
	"github.com/cloudfoundry/dropsonde"
	"github.com/pivotal-golang/lager"

	cf_lager "github.com/cloudfoundry-incubator/cf-lager"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/http_server"
	"github.com/tedsuo/ifrit/sigmon"
	"github.com/tedsuo/rata"
)

const DEFAULT_ETCD_WORKERS = 25

var maxTTL = flag.Int("maxTTL", 120, "Maximum TTL on the route")
var port = flag.Uint("port", 8080, "Port to run rounting-api server on")
var configPath = flag.String("config", "", "Configuration for routing-api")
var devMode = flag.Bool("devMode", false, "Disable authentication for easier development iteration")
var ip = flag.String("ip", "", "The public ip of the routing api")
var systemDomain = flag.String("systemDomain", "", "System domain that the routing api should register on")

func route(f func(w http.ResponseWriter, r *http.Request)) http.Handler {
	return http.HandlerFunc(f)
}

func main() {
	logger := cf_lager.New("routing-api")

	err := checkFlags()
	if err != nil {
		logger.Error("failed to start", err)
		os.Exit(1)
	}

	cfg, err := config.NewConfigFromFile(*configPath)
	if err != nil {
		logger.Error("failed to start", err)
		os.Exit(1)
	}

	err = dropsonde.Initialize(cfg.MetronConfig.Address+":"+cfg.MetronConfig.Port, cfg.LogGuid)
	if err != nil {
		logger.Error("failed to initialize Dropsonde", err)
		os.Exit(1)
	}

	if cfg.DebugAddress != "" {
		cf_debug_server.Run(cfg.DebugAddress)
	}

	database, err := initializeDatabase(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize database", err)
		os.Exit(1)
	}

	err = database.Connect()
	if err != nil {
		logger.Error("failed to connect to database", err)
		os.Exit(1)
	}
	defer database.Disconnect()

	prefix := "routing_api"
	statsdClient, err := statsd.NewBufferedClient(cfg.StatsdEndpoint, prefix, cfg.StatsdClientFlushInterval, 512)
	if err != nil {
		logger.Error("failed to create a statsd client", err)
		os.Exit(1)
	}
	defer statsdClient.Close()

	stopChan := make(chan struct{})
	apiServer := constructApiServer(cfg, database, statsdClient, stopChan, logger)
	stopper := constructStopper(stopChan)

	routerRegister := constructRouteRegister(cfg.LogGuid, database, logger)

	metricsTicker := time.NewTicker(cfg.MetricsReportingInterval)
	metricsReporter := metrics.NewMetricsReporter(database, statsdClient, metricsTicker)

	members := grouper.Members{
		{"metrics", metricsReporter},
		{"api-server", apiServer},
		{"conn-stopper", stopper},
		{"route-register", routerRegister},
	}

	group := grouper.NewOrdered(os.Interrupt, members)
	process := ifrit.Invoke(sigmon.New(group))

	// This is used by testrunner to signal ready for tests.
	logger.Info("started", lager.Data{"port": *port})

	errChan := process.Wait()
	err = <-errChan
	if err != nil {
		logger.Error("shutdown-error", err)
		os.Exit(1)
	}
	logger.Info("exited")
}

func constructStopper(stopChan chan struct{}) ifrit.Runner {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		close(ready)
		select {
		case <-signals:
			close(stopChan)
		}

		return nil
	})
}

func constructRouteRegister(logGuid string, database db.DB, logger lager.Logger) ifrit.Runner {
	host := fmt.Sprintf("api.%s/routing", *systemDomain)
	route := db.Route{
		Route:   host,
		Port:    uint16(*port),
		IP:      *ip,
		TTL:     *maxTTL,
		LogGuid: logGuid,
	}

	registerInterval := *maxTTL / 2
	ticker := time.NewTicker(time.Duration(registerInterval) * time.Second)

	return helpers.NewRouteRegister(database, route, ticker, logger)
}

func constructApiServer(cfg config.Config, database db.DB, statsdClient statsd.Statter, stopChan chan struct{}, logger lager.Logger) ifrit.Runner {
	var token authentication.Token

	if *devMode {
		token = authentication.NullToken{}
	} else {
		token = authentication.NewAccessToken(cfg.UAAPublicKey)
		err := token.CheckPublicToken()
		if err != nil {
			logger.Error("failed to check public token", err)
			os.Exit(1)
		}
	}

	validator := handlers.NewValidator()
	routesHandler := handlers.NewRoutesHandler(token, *maxTTL, validator, database, logger)
	eventStreamHandler := handlers.NewEventStreamHandler(token, database, logger, statsdClient, stopChan)
	routeGroupsHandler := handlers.NewRouteGroupsHandler(token, logger)
	tcpMappingsHandler := handlers.NewTcpRouteMappingsHandler(token, validator, database, logger)

	actions := rata.Handlers{
		routing_api.UpsertRoute:           route(routesHandler.Upsert),
		routing_api.DeleteRoute:           route(routesHandler.Delete),
		routing_api.ListRoute:             route(routesHandler.List),
		routing_api.EventStreamRoute:      route(eventStreamHandler.EventStream),
		routing_api.ListRouterGroups:      route(routeGroupsHandler.ListRouterGroups),
		routing_api.UpsertTcpRouteMapping: route(tcpMappingsHandler.Upsert),
		routing_api.ListTcpRouteMapping:   route(tcpMappingsHandler.List),
	}

	handler, err := rata.NewRouter(routing_api.Routes, actions)
	if err != nil {
		logger.Error("failed to create router", err)
		os.Exit(1)
	}

	handler = handlers.LogWrap(handler, logger)
	return http_server.New(":"+strconv.Itoa(int(*port)), handler)
}

func initializeDatabase(cfg config.Config, logger lager.Logger) (db.DB, error) {
	logger.Info("database", lager.Data{"etcd-addresses": flag.Args()})
	maxWorkers := cfg.MaxConcurrentETCDRequests
	if maxWorkers <= 0 {
		maxWorkers = DEFAULT_ETCD_WORKERS
	}

	return db.NewETCD(flag.Args(), maxWorkers)
}

func checkFlags() error {
	flag.Parse()
	if *configPath == "" {
		return errors.New("No configuration file provided")
	}

	if *ip == "" {
		return errors.New("No ip address provided")
	}

	if *systemDomain == "" {
		return errors.New("No system domain provided")
	}

	if *port > 65535 {
		return errors.New("Port must be in range 0 - 65535")
	}

	return nil
}
