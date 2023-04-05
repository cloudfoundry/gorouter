package integration

import (
	"bytes"
	"fmt"
	"net/http"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

	Context("when gorouter talks to a broken app behind envoy", func() {
		var appURL string
		var badApp *common.TcpApp

		BeforeEach(func() {
			appURL = "bad-app." + test_util.LocalhostDNS

			badApp = common.NewTcpApp([]route.Uri{route.Uri(appURL)}, testState.cfg.Port, testState.mbusClient, nil, "")
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

			req := testState.newPostRequest(
				fmt.Sprintf("http://%s", appURL),
				bytes.NewReader([]byte(payload)),
			)
			resp, err := testState.client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			if resp.Body != nil {
				resp.Body.Close()
			}
		})
	})
})
