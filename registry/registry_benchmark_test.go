package registry_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/metric_sender"
	"github.com/cloudfoundry/dropsonde/metricbatcher"

	"code.cloudfoundry.org/gorouter/config"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
)

var logger = setupLogger()
var configObj = setupConfig()

var _ = dropsonde.Initialize(configObj.Logging.MetronAddress, configObj.Logging.JobName)
var sender = metric_sender.NewMetricSender(dropsonde.AutowiredEmitter())
var batcher = metricbatcher.New(sender, 5*time.Second)
var reporter = &metrics.MetricsReporter{Sender: sender, Batcher: batcher}

var fooEndpoint = route.NewEndpoint(&route.EndpointOpts{})

func setupLogger() *test_util.TestLogger {
	tmpLogger := test_util.NewTestLogger("test")
	log.SetLoggingLevel("Warn")
	return tmpLogger
}
func setupConfig() *config.Config {
	c, err := config.DefaultConfig()
	if err != nil {
		panic(err)
	}

	c.PruneStaleDropletsInterval = 50 * time.Millisecond
	c.DropletStaleThreshold = 24 * time.Millisecond
	c.IsolationSegments = []string{"foo", "bar"}
	return c
}
func BenchmarkRegisterWith100KRoutes(b *testing.B) {
	r := registry.NewRouteRegistry(logger.Logger, configObj, reporter)

	for i := 0; i < 100000; i++ {
		r.Register(route.Uri(fmt.Sprintf("foo%d.example.com", i)), fooEndpoint)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.Register("foo50000.example.com", fooEndpoint)
	}
	b.ReportAllocs()
}

func BenchmarkRegisterWithOneRoute(b *testing.B) {
	r := registry.NewRouteRegistry(logger.Logger, configObj, reporter)

	r.Register("foo.example.com", fooEndpoint)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.Register("foo.example.com", fooEndpoint)
	}
	b.ReportAllocs()
}

func BenchmarkRegisterWithConcurrentLookupWith100kRoutes(b *testing.B) {
	r := registry.NewRouteRegistry(logger.Logger, configObj, reporter)
	maxRoutes := 100000
	routeUris := make([]route.Uri, maxRoutes)

	for i := 0; i < maxRoutes; i++ {
		routeUris[i] = route.Uri(fmt.Sprintf("foo%d.example.com", i))
		r.Register(routeUris[i], fooEndpoint)
	}

	lookupCounts := make(chan uint)

	ctx, cancel := context.WithCancel(context.Background())
	numLookupers := 10
	var wg sync.WaitGroup
	for i := 0; i < numLookupers; i++ {
		wg.Add(1)
		go func(ctx context.Context, wg *sync.WaitGroup) {
			var lookups uint
			for i := 0; ; i++ {
				select {
				case <-ctx.Done():
					wg.Done()
					lookupCounts <- lookups
					return
				default:
					routeUri := routeUris[i%maxRoutes]
					r.Lookup(routeUri)
					lookups++
				}
			}
		}(ctx, &wg)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.Register(routeUris[i%maxRoutes], fooEndpoint)
	}
	b.Elapsed()

	cancel()
	wg.Wait()

	var lookupCount uint
	for i := 0; i < numLookupers; i++ {
		lookupCount += <-lookupCounts
	}

	b.Logf("Looked up %d routes concurrently, registered %d", lookupCount, b.N)
	b.ReportAllocs()
}
