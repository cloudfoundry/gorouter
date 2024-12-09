package integration

import (
	"bytes"
	"fmt"
	"net"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("Retries", func() {
	var (
		testState *testState
	)

	BeforeEach(func() {
		testState = NewTestState()
		testState.cfg.Backends.MaxAttempts = 15
		testState.StartGorouterOrFail()
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
	})

	Context("when gorouter is called by a bad client", func() {
		var appURL string
		var app *common.TestApp

		BeforeEach(func() {
			appURL = "bad-app." + test_util.LocalhostDNS

			app = common.NewTestApp([]route.Uri{route.Uri(appURL)}, testState.cfg.Port, testState.mbusClient, nil, "")
			app.TlsRegister(testState.trustedBackendServerCertSAN)

			app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			})

			errChan := app.TlsListen(testState.trustedBackendTLSConfig)
			Consistently(errChan).ShouldNot(Receive())
		})

		AfterEach(func() {
			app.Stop()
		})

		It("does not prune the endpoint on context cancelled", func() {
			conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", appURL, testState.cfg.Port))
			Expect(err).ToNot(HaveOccurred())

			_, err = conn.Write([]byte(fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\n\r\n", appURL)))
			Expect(err).ToNot(HaveOccurred())

			_ = conn.Close()

			Consistently(func() bool {
				res, err := testState.client.Do(testState.newGetRequest("http://" + appURL))
				return err == nil && res.StatusCode == http.StatusTeapot
			}).Should(Equal(true))
		})
	})

	Context("when gorouter talks to a broken app behind envoy", func() {
		var appURL string
		var badApp *common.TcpApp

		BeforeEach(func() {
			appURL = "bad-app." + test_util.LocalhostDNS

			badApp = common.NewTcpApp([]route.Uri{route.Uri(appURL)}, test_util.NextAvailPort(), testState.mbusClient, nil, "")
			badApp.Register()
		})

		AfterEach(func() {
			badApp.Stop()
		})

		It("retries POST requests when no data was written", func() {
			payload := "this could be a meaningful body"

			var handlers []func(conn *test_util.HttpConn)

			closeOnAccept := func(conn *test_util.HttpConn) {
				conn.Close()
			}

			respondOK := func(conn *test_util.HttpConn) {
				_, err := http.ReadRequest(conn.Reader)
				Expect(err).NotTo(HaveOccurred())

				resp := test_util.NewResponse(http.StatusOK)
				conn.WriteResponse(resp)
				conn.Close()
			}

			for i := 0; i < 14; i++ {
				handlers = append(handlers, closeOnAccept)
			}

			handlers = append(handlers, respondOK)
			badApp.SetHandlers(handlers)
			badApp.Listen()

			successSeen := false
			// we need to retry the entire http request many times to get a success until https://github.com/golang/go/issues/31259
			// is resolved.
			for i := 0; i < 100; i++ {
				req := testState.newPostRequest(
					fmt.Sprintf("http://%s", appURL),
					bytes.NewReader([]byte(payload)),
				)
				resp, err := testState.client.Do(req)
				Expect(err).NotTo(HaveOccurred())
				if resp.StatusCode == http.StatusOK {
					successSeen = true
					break
				}
				if resp.Body != nil {
					resp.Body.Close()
				}
			}
			Expect(successSeen).To(BeTrue())
		})
	})
})
