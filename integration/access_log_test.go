package integration

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/test/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Access Log", func() {
	var (
		testState *testState
		done      chan bool
		logs      <-chan string
	)

	BeforeEach(func() {
		testState = NewTestState()
	})

	JustBeforeEach(func() {
		testState.StartGorouterOrFail()
	})

	AfterEach(func() {
		testState.StopAndCleanup()
	})

	Context("when using syslog", func() {
		BeforeEach(func() {
			// disable file logging
			testState.cfg.AccessLog.EnableStreaming = true
			testState.cfg.AccessLog.File = ""
			// generic tag
			testState.cfg.Logging.Syslog = "gorouter"
		})

		Context("via UDP", func() {
			BeforeEach(func() {
				testState.cfg.Logging.SyslogNetwork = "udp"
				done = make(chan bool)
				testState.cfg.Logging.SyslogAddr, logs = common.TestUdp(done)
			})

			AfterEach(func() {
				close(done)
			})

			It("properly emits access logs", func() {
				req := testState.newGetRequest("https://foobar.cloudfoundry.org")
				res, err := testState.client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusNotFound))

				log := <-logs

				Expect(log).To(ContainSubstring(`x_cf_routererror:"unknown_route"`))
				Expect(log).To(ContainSubstring(`"GET / HTTP/1.1" 404`))
				Expect(log).To(ContainSubstring("foobar.cloudfoundry.org"))

				// ensure we don't see any excess access logs
				Consistently(func() int { return len(logs) }).Should(Equal(0))
			})
		})

		Context("via TCP", func() {
			BeforeEach(func() {
				testState.cfg.Logging.SyslogNetwork = "tcp"
				done = make(chan bool)
				testState.cfg.Logging.SyslogAddr, logs = common.TestTcp(done)
			})

			AfterEach(func() {
				close(done)
			})

			It("properly emits successful requests", func() {
				req := testState.newGetRequest("https://foobar.cloudfoundry.org")
				res, err := testState.client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusNotFound))

				log := <-logs

				Expect(log).To(ContainSubstring(`x_cf_routererror:"unknown_route"`))
				Expect(log).To(ContainSubstring(`"GET / HTTP/1.1" 404`))
				Expect(log).To(ContainSubstring("foobar.cloudfoundry.org"))

				// ensure we don't see any excess access logs
				Consistently(func() int { return len(logs) }).Should(Equal(0))
			})
		})
	})
})
