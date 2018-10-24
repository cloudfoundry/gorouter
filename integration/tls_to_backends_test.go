package integration

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("TLS to backends", func() {
	var (
		testState           *testState
		serverCertDomainSAN string
		backendCertChain    test_util.CertChain
		clientCertChain     test_util.CertChain
		backendTLSConfig    *tls.Config
	)

	BeforeEach(func() {
		testState = NewTestState()
		testState.cfg.SkipSSLValidation = false
		testState.cfg.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA}

		serverCertDomainSAN = "example.com"
		backendCertChain = test_util.CreateSignedCertWithRootCA(test_util.CertNames{CommonName: serverCertDomainSAN})
		testState.cfg.CACerts = string(backendCertChain.CACertPEM)

		clientCertChain = test_util.CreateSignedCertWithRootCA(test_util.CertNames{CommonName: "gorouter"})
		backendTLSConfig = backendCertChain.AsTLSConfig()
		backendTLSConfig.ClientAuth = tls.RequireAndVerifyClientCert

		// set Gorouter to use client certs
		testState.cfg.Backends.TLSPem = config.TLSPem{
			CertChain:  string(clientCertChain.CertPEM),
			PrivateKey: string(clientCertChain.PrivKeyPEM),
		}

		// make backend trust the CA that signed the gorouter's client cert
		certPool := x509.NewCertPool()
		certPool.AddCert(clientCertChain.CACert)
		backendTLSConfig.ClientCAs = certPool

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

			Eventually(func() bool { return appRegistered(routesURI, wsApp) }, "2s").Should(BeTrue())

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
			wsApp.TlsRegister(serverCertDomainSAN)
			wsApp.TlsListen(backendTLSConfig)

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

			responseChan := make(chan *http.Response)
			go func() {
				defer GinkgoRecover()
				var resp *http.Response
				resp, err = http.ReadResponse(x.Reader, &http.Request{})
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				responseChan <- resp
			}()

			var resp *http.Response
			Eventually(responseChan, "9s").Should(Receive(&resp))
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
		runningApp1.TlsRegister(serverCertDomainSAN)
		runningApp1.TlsListen(backendTLSConfig)
		heartbeatInterval := 200 * time.Millisecond
		runningTicker := time.NewTicker(heartbeatInterval)
		go func() {
			for {
				<-runningTicker.C
				runningApp1.TlsRegister(serverCertDomainSAN)
			}
		}()
		routesURI := fmt.Sprintf("http://%s:%s@%s:%d/routes", testState.cfg.Status.User, testState.cfg.Status.Pass, "localhost", testState.cfg.Status.Port)

		Eventually(func() bool { return appRegistered(routesURI, runningApp1) }, "2s").Should(BeTrue())
		runningApp1.VerifyAppStatus(200)
	})
})
