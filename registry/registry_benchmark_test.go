package registry_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/metric_sender"
	"github.com/cloudfoundry/dropsonde/metricbatcher"
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega/gbytes"
	"github.com/uber-go/zap"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/routing-api/models"
)

var testLogger = setupLogger()
var configObj = setupConfig()

var _ = dropsonde.Initialize(configObj.Logging.MetronAddress, configObj.Logging.JobName)
var sender = metric_sender.NewMetricSender(dropsonde.AutowiredEmitter())
var batcher = metricbatcher.New(sender, 5*time.Second)
var reporter = metrics.NewMetricsReporter(sender, batcher)

var fooEndpoint = route.NewEndpoint(
	"12345", "192.168.1.1", 1234, "id1", "0", map[string]string{}, -1, "",
	models.ModificationTag{}, "", false,
)

func setupLogger() logger.Logger {
	sink := &test_util.TestZapSink{
		Buffer: gbytes.NewBuffer(),
	}
	l := logger.NewLogger(
		"test",
		zap.InfoLevel,
		zap.Output(zap.MultiWriteSyncer(sink, zap.AddSync(ginkgo.GinkgoWriter))),
		zap.ErrorOutput(zap.MultiWriteSyncer(sink, zap.AddSync(ginkgo.GinkgoWriter))),
	)
	return &test_util.TestZapLogger{
		Logger:      l,
		TestZapSink: sink,
	}
}
func setupConfig() *config.Config {
	c := config.DefaultConfig()
	c.PruneStaleDropletsInterval = 50 * time.Millisecond
	c.DropletStaleThreshold = 24 * time.Millisecond
	c.IsolationSegments = []string{"foo", "bar"}
	return c
}
func BenchmarkRegisterWith100KRoutes(b *testing.B) {
	r := registry.NewRouteRegistry(testLogger, configObj, reporter)

	for i := 0; i < 100000; i++ {
		r.Register(route.Uri(fmt.Sprintf("foo%d.example.com", i)), fooEndpoint)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.Register("foo50000.example.com", fooEndpoint)
	}
}

func BenchmarkRegisterWithOneRoute(b *testing.B) {
	r := registry.NewRouteRegistry(testLogger, configObj, reporter)

	r.Register("foo.example.com", fooEndpoint)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.Register("foo.example.com", fooEndpoint)
	}
}
