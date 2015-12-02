package metrics

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry/storeadapter"
)

type PartialStatsdClient interface {
	GaugeDelta(stat string, value int64, rate float32) error
	Gauge(stat string, value int64, rate float32) error
}

type MetricsReporter struct {
	db       db.DB
	stats    PartialStatsdClient
	ticker   *time.Ticker
	doneChan chan bool
}

func NewMetricsReporter(database db.DB, stats PartialStatsdClient, ticker *time.Ticker) *MetricsReporter {
	return &MetricsReporter{db: database, stats: stats, ticker: ticker}
}

func (r *MetricsReporter) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	httpEventChan, _, httpErrChan := r.db.WatchRouteChanges(db.HTTP_ROUTE_BASE_KEY)
	tcpEventChan, _, tcpErrChan := r.db.WatchRouteChanges(db.TCP_MAPPING_BASE_KEY)
	close(ready)
	ready = nil

	r.stats.Gauge("total_subscriptions", 0, 1.0)
	r.stats.Gauge("total_tcp_subscriptions", 0, 1.0)

	for {
		select {
		case event := <-httpEventChan:
			statsDelta := getStatsEventType(event)
			r.stats.GaugeDelta("total_routes", statsDelta, 1.0)
		case event := <-tcpEventChan:
			statsDelta := getStatsEventType(event)
			r.stats.GaugeDelta("total_tcp_routes", statsDelta, 1.0)
		case <-r.ticker.C:
			r.stats.Gauge("total_routes", r.getTotalRoutes(), 1.0)
			r.stats.GaugeDelta("total_subscriptions", 0, 1.0)
			r.stats.Gauge("total_tcp_routes", r.getTotalTcpRoutes(), 1.0)
			r.stats.GaugeDelta("total_tcp_subscriptions", 0, 1.0)
		case <-signals:
			return nil
		case err := <-httpErrChan:
			return err
		case err := <-tcpErrChan:
			return err
		}
	}
}

func (r MetricsReporter) getTotalRoutes() int64 {
	routes, _ := r.db.ReadRoutes()
	return int64(len(routes))
}

func (r MetricsReporter) getTotalTcpRoutes() int64 {
	routes, _ := r.db.ReadTcpRouteMappings()
	return int64(len(routes))
}

func getStatsEventType(event storeadapter.WatchEvent) int64 {
	if event.PrevNode == nil && event.Type == storeadapter.UpdateEvent {
		return 1
	} else if event.Type == storeadapter.ExpireEvent || event.Type == storeadapter.DeleteEvent {
		return -1
	} else {
		return 0
	}
}
