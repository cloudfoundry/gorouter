package monitor_test

import (
	"time"

	"github.com/cloudfoundry/sonde-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	"code.cloudfoundry.org/gorouter/metrics/monitor"
	"code.cloudfoundry.org/gorouter/test_util"
)

const (
	interval = 100 * time.Millisecond
)

var _ = Describe("Uptime", func() {
	var (
		uptime *monitor.Uptime
		logger *test_util.TestLogger
	)

	BeforeEach(func() {
		logger = test_util.NewTestLogger("test")
		fakeEventEmitter.Reset()
		uptime = monitor.NewUptime(interval, logger.Logger)
		go uptime.Start()
	})

	Context("stops automatically", func() {

		AfterEach(func() {
			uptime.Stop()
		})

		It("returns a value metric containing uptime after specified time", func() {
			Eventually(fakeEventEmitter.GetMessages).Should(HaveLen(1))

			metric := fakeEventEmitter.GetMessages()[0].Event.(*events.ValueMetric)
			Expect(metric.Name).To(Equal(proto.String("uptime")))
			Expect(metric.Unit).To(Equal(proto.String("seconds")))
		})

		It("reports increasing uptime value", func() {
			Eventually(fakeEventEmitter.GetMessages).Should(HaveLen(1))
			metric := fakeEventEmitter.GetMessages()[0].Event.(*events.ValueMetric)
			uptime := *(metric.Value)

			Eventually(getLatestUptime, "2s").Should(BeNumerically(">", uptime))
		})
	})

	It("stops the monitor and respective ticker", func() {
		Eventually(func() int { return len(fakeEventEmitter.GetMessages()) }).Should(BeNumerically(">=", 1))

		uptime.Stop()

		current := getLatestUptime()
		Consistently(getLatestUptime, 2).Should(Equal(current))
	})
})

func getLatestUptime() float64 {
	lastMsgIndex := len(fakeEventEmitter.GetMessages()) - 1
	return *fakeEventEmitter.GetMessages()[lastMsgIndex].Event.(*events.ValueMetric).Value
}
