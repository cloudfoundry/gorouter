package integration

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TLS to backends", func() {
	var (
		testState *testState
	)

	BeforeEach(func() {
		testState = NewTestState()
		testState.EnableAccessLog()
	})

	JustBeforeEach(func() {
		testState.StartGorouterOrFail()
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
	})

	Context("websockets and TLS interaction", func() {
		assertWebsocketSuccess := func(wsApp *common.TestApp) {
			routesURI := fmt.Sprintf("http://%s:%s@%s:%d/routes", testState.cfg.Status.User, testState.cfg.Status.Pass, "localhost", testState.cfg.Status.Routes.Port)

			Eventually(func() bool { return appRegistered(routesURI, wsApp) }, "2s", "500ms").Should(BeTrue())

			wsApp.WaitUntilReady()

			conn, err := net.Dial("tcp", fmt.Sprintf("ws-app.%s:%d", test_util.LocalhostDNS, testState.cfg.Port))
			Expect(err).NotTo(HaveOccurred())

			x := test_util.NewHttpConn(conn)

			req := test_util.NewRequest("GET", "ws-app."+test_util.LocalhostDNS, "/chat", nil)
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "upgrade")
			x.WriteRequest(req)

			resp, _ := x.ReadResponse()
			Expect(resp.StatusCode).To(Equal(http.StatusSwitchingProtocols))

			x.WriteLine("hello from client")
			x.CheckLine("hello from server")

			x.Close()
		}

		It("successfully connects with both websockets and TLS to backends", func() {
			wsApp := test.NewWebSocketApp([]route.Uri{"ws-app." + test_util.LocalhostDNS}, testState.cfg.Port, testState.mbusClient, time.Millisecond, "")
			wsApp.TlsRegister(testState.trustedBackendServerCertSAN)
			wsApp.TlsListen(testState.trustedBackendTLSConfig)

			assertWebsocketSuccess(wsApp)
		})

		It("successfully connects with websockets but not TLS to backends", func() {
			wsApp := test.NewWebSocketApp([]route.Uri{"ws-app." + test_util.LocalhostDNS}, testState.cfg.Port, testState.mbusClient, time.Millisecond, "")
			wsApp.Register()
			wsApp.Listen()

			assertWebsocketSuccess(wsApp)
		})

		// this test mandates RFC 6455 - https://datatracker.ietf.org/doc/html/rfc6455#section-4
		// where it is stated that:
		// "(...) If the status code received from the server is not 101, the
		//       client handles the response per HTTP [RFC2616] procedures."
		// Which means the proxy must treat non-101 responses as regular HTTP [ and not close the connection per se ]
		It("does not close connections with backends that respond with non 101-status code", func() {
			wsApp := test.NewNotUpgradingWebSocketApp([]route.Uri{"ws-app." + test_util.LocalhostDNS}, testState.cfg.Port, testState.mbusClient, "")
			wsApp.Register()
			wsApp.Listen()

			routesURI := fmt.Sprintf("http://%s:%s@%s:%d/routes", testState.cfg.Status.User, testState.cfg.Status.Pass, localIP, testState.cfg.Status.Routes.Port)

			Eventually(func() bool { return appRegistered(routesURI, wsApp) }, "2s").Should(BeTrue())

			wsApp.WaitUntilReady()

			conn, err := net.Dial("tcp", fmt.Sprintf("ws-app.%s:%d", test_util.LocalhostDNS, testState.cfg.Port))
			Expect(err).NotTo(HaveOccurred())

			x := test_util.NewHttpConn(conn)

			req := test_util.NewRequest("GET", "ws-app."+test_util.LocalhostDNS, "/chat", nil)
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "upgrade")
			x.WriteRequest(req)

			resp, err := http.ReadResponse(x.Reader, &http.Request{})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(404))

			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()

			Expect(string(data)).To(ContainSubstring("beginning of the response body goes here"))

			x.Close()
		})
	})

	It("successfully establishes a mutual TLS connection with backend", func() {
		runningApp1 := test.NewGreetApp([]route.Uri{"some-app-expecting-client-certs." + test_util.LocalhostDNS}, testState.cfg.Port, testState.mbusClient, nil)
		runningApp1.TlsRegister(testState.trustedBackendServerCertSAN)
		runningApp1.TlsListen(testState.trustedBackendTLSConfig)

		routesURI := fmt.Sprintf("http://%s:%s@%s:%d/routes", testState.cfg.Status.User, testState.cfg.Status.Pass, "localhost", testState.cfg.Status.Routes.Port)

		Eventually(func() bool { return appRegistered(routesURI, runningApp1) }, "2s").Should(BeTrue())
		runningApp1.VerifyAppStatus(200)
	})

	It("logs an access log with valid timestamps", func() {
		// registering a route setup
		runningApp1 := test.NewGreetApp([]route.Uri{"some-app-expecting-client-certs." + test_util.LocalhostDNS}, testState.cfg.Port, testState.mbusClient, nil)
		runningApp1.TlsRegister(testState.trustedBackendServerCertSAN)
		runningApp1.TlsListen(testState.trustedBackendTLSConfig)
		routesURI := fmt.Sprintf("http://%s:%s@%s:%d/routes", testState.cfg.Status.User, testState.cfg.Status.Pass, "localhost", testState.cfg.Status.Routes.Port)
		Eventually(func() bool { return appRegistered(routesURI, runningApp1) }, "2s").Should(BeTrue())
		runningApp1.VerifyAppStatus(200)

		Eventually(func() ([]byte, error) {
			return os.ReadFile(testState.AccessLogFilePath())
		}).Should(ContainSubstring(`response_time`))

		f, err := os.ReadFile(testState.AccessLogFilePath())
		Expect(err).NotTo(HaveOccurred())
		fmt.Printf("contents %s", f)

		responseTime := parseTimestampsFromAccessLog("response_time", f)
		gorouterTime := parseTimestampsFromAccessLog("gorouter_time", f)

		Expect(responseTime).To(BeNumerically(">", 0))
		Expect(gorouterTime).To(BeNumerically(">", 0))
	})
})

func parseTimestampsFromAccessLog(keyName string, bytesToParse []byte) float64 {
	exp := regexp.MustCompile(keyName + `:(\d+\.?\d*)`)
	value, err := strconv.ParseFloat(string(exp.FindSubmatch(bytesToParse)[1]), 64)
	Expect(err).NotTo(HaveOccurred())
	return value
}
