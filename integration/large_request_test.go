package integration

import (
	"fmt"
	"os"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Large requests", func() {
	var (
		testState *testState
		appURL    string
		echoApp   *common.TestApp
	)

	BeforeEach(func() {
		testState = NewTestState()
		testState.EnableAccessLog()
		testState.EnableMetron()
		testState.StartGorouterOrFail()

		appURL = "echo-app." + test_util.LocalhostDNS

		echoApp = newEchoApp([]route.Uri{route.Uri(appURL)}, testState.cfg.Port, testState.mbusClient, time.Millisecond, "")
		echoApp.TlsRegister(testState.trustedBackendServerCertSAN)
		echoApp.TlsListen(testState.trustedBackendTLSConfig)
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
	})

	It("logs requests that exceed the MaxHeaderBytes configuration (but are lower than 1MB)", func() {
		pathSize := 2 * 1024 // 2kb
		path := strings.Repeat("a", pathSize)

		req := testState.newRequest(fmt.Sprintf("http://%s/%s", appURL, path))

		resp, err := testState.client.Do(req)
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.StatusCode).To(Equal(431))

		getAccessLogContents := func() string {
			accessLogContents, err := os.ReadFile(testState.AccessLogFilePath())
			Expect(err).NotTo(HaveOccurred())
			return string(accessLogContents)
		}

		Eventually(getAccessLogContents).Should(MatchRegexp("echo-app.*/aaaaaaaa.*431.*x_cf_routererror:\"max-request-size-exceeded\""))
		Eventually(func() []string {
			var messages []string
			events := testState.MetronEvents()
			for _, event := range events {
				if event.EventType == "LogMessage" {
					messages = append(messages, event.Name)
				}
			}
			return messages
		}).Should(ContainElement(MatchRegexp("echo-app.*/aaaaaaaa.*431.*x_cf_routererror:\"max-request-size-exceeded\"")))
	})
})
