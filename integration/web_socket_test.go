package integration

import (
	"fmt"
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

var _ = Describe("Websockets", func() {
	var (
		testState *testState
	)

	BeforeEach(func() {
		testState = NewTestState()
		testState.StartGorouterOrFail()
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
	})

	Context("When gorouter attempts to connect to a websocket app that fails", func() {
		assertWebsocketFailure := func(wsApp *common.TestApp) {
			routesURI := fmt.Sprintf("http://%s:%s@%s:%d/routes", testState.cfg.Status.User, testState.cfg.Status.Pass, "localhost", testState.cfg.Status.Port)

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
			Expect(resp.StatusCode).To(Equal(http.StatusBadGateway))

			x.Close()
		}

		It("returns a status code indicating failure", func() {
			wsApp := test.NewFailingWebSocketApp([]route.Uri{"ws-app." + test_util.LocalhostDNS}, testState.cfg.Port, testState.mbusClient, time.Millisecond, "")
			wsApp.TlsRegister(testState.trustedBackendServerCertSAN)
			wsApp.TlsListen(testState.trustedBackendTLSConfig)

			assertWebsocketFailure(wsApp)
		})

	})

})
