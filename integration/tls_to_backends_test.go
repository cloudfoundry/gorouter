package integration

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("TLS to backends", func() {
	var (
		testState *testState
	)

	BeforeEach(func() {
		testState = NewTestState()
		testState.StartGorouter()
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
	})

	Context("websockets and TLS interaction", func() {
		assertWebsocketSuccess := func(wsApp *common.TestApp) {
			routesURI := fmt.Sprintf("http://%s:%s@%s:%d/routes", testState.cfg.Status.User, testState.cfg.Status.Pass, "localhost", testState.cfg.Status.Port)

			Eventually(func() bool { return appRegistered(routesURI, wsApp) }, "2s", "500ms").Should(BeTrue())


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

		It("closes connections with backends that respond with non 101-status code", func() {
			wsApp := test.NewHangingWebSocketApp([]route.Uri{"ws-app." + test_util.LocalhostDNS}, testState.cfg.Port, testState.mbusClient, "")
			wsApp.Register()
			wsApp.Listen()

			routesURI := fmt.Sprintf("http://%s:%s@%s:%d/routes", testState.cfg.Status.User, testState.cfg.Status.Pass, localIP, testState.cfg.Status.Port)

			Eventually(func() bool { return appRegistered(routesURI, wsApp) }, "2s").Should(BeTrue())

			conn, err := net.Dial("tcp", fmt.Sprintf("ws-app.%s:%d", test_util.LocalhostDNS, testState.cfg.Port))
			Expect(err).NotTo(HaveOccurred())

			x := test_util.NewHttpConn(conn)

			req := test_util.NewRequest("GET", "ws-app."+test_util.LocalhostDNS, "/chat", nil)
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Connection", "upgrade")
			x.WriteRequest(req)

			resp, err := http.ReadResponse(x.Reader, &http.Request{})
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(404))

			// client-side conn should have been closed
			// we verify this by trying to read from it, and checking that
			//  - the read does not block
			//  - the read returns no data
			//  - the read returns an error EOF
			n, err := conn.Read(make([]byte, 100))
			Expect(n).To(Equal(0))
			Expect(err).To(Equal(io.EOF))

			x.Close()
		})
	})

	It("successfully establishes a mutual TLS connection with backend", func() {
		runningApp1 := test.NewGreetApp([]route.Uri{"some-app-expecting-client-certs." + test_util.LocalhostDNS}, testState.cfg.Port, testState.mbusClient, nil)
		runningApp1.TlsRegister(testState.trustedBackendServerCertSAN)
		runningApp1.TlsListen(testState.trustedBackendTLSConfig)
		runningApp1.TlsRegister(testState.trustedBackendServerCertSAN)

		routesURI := fmt.Sprintf("http://%s:%s@%s:%d/routes", testState.cfg.Status.User, testState.cfg.Status.Pass, "localhost", testState.cfg.Status.Port)

		Eventually(func() bool { return appRegistered(routesURI, runningApp1) }, "2s").Should(BeTrue())
		runningApp1.VerifyAppStatus(200)
	})
})
