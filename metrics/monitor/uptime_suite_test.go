package monitor_test

import (
	"github.com/cloudfoundry/dropsonde/emitter/fake"
	"github.com/cloudfoundry/dropsonde/metric_sender"
	"github.com/cloudfoundry/dropsonde/metrics"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestMonitor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Monitor Suite")
}

var (
	fakeEventEmitter *fake.FakeEventEmitter
)

var _ = BeforeSuite(func() {
	fakeEventEmitter = fake.NewFakeEventEmitter("MonitorTest")
	sender := metric_sender.NewMetricSender(fakeEventEmitter)
	//batcher := metricbatcher.New(sender, 100*time.Millisecond)
	metrics.Initialize(sender, nil)
})

var _ = AfterSuite(func() {
	fakeEventEmitter.Close()
})
