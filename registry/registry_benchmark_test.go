package registry_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/metric_sender"
	"github.com/cloudfoundry/dropsonde/metricbatcher"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega/gbytes"
	"github.com/uber-go/zap"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
)

var testLogger = setupLogger()
var configObj = setupConfig()

var _ = dropsonde.Initialize(configObj.Logging.MetronAddress, configObj.Logging.JobName)
var sender = metric_sender.NewMetricSender(dropsonde.AutowiredEmitter())
var batcher = metricbatcher.New(sender, 5*time.Second)
var reporter = &metrics.MetricsReporter{Sender: sender, Batcher: batcher}

var fooEndpoint = route.NewEndpoint(&route.EndpointOpts{})

func setupLogger() logger.Logger {
	sink := &test_util.TestZapSink{
		Buffer: gbytes.NewBuffer(),
	}
	l := logger.NewLogger(
		"test",
		"unix-epoch",
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
