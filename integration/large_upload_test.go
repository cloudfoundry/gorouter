package integration

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test/common"
	"code.cloudfoundry.org/gorouter/test_util"
	nats "github.com/nats-io/nats.go"
	"github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Large upload", func() {
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

	Context("when a client tries to upload a large file", func() {
		var appURL string
		var echoApp *common.TestApp

		BeforeEach(func() {
			appURL = "echo-app." + test_util.LocalhostDNS

			echoApp = newEchoApp([]route.Uri{route.Uri(appURL)}, testState.cfg.Port, testState.mbusClient, time.Millisecond, "")
			echoApp.TlsRegister(testState.trustedBackendServerCertSAN)
			echoApp.TlsListen(testState.trustedBackendTLSConfig)
		})

		It("the connection remains open for the entire upload", func() {
			// We are afraid that this test might become flaky at some point
			// If it does, try increasing the size of the payload
			// or maybe decreasing it...

			// We have empirically tested that this number needs to be quite large in
			// order for the test to be testing the right thing

			payloadSize := 2 << 24
			// 2^24 ~= 17Mb

			payload := strings.Repeat("a", payloadSize)

			req := testState.newPostRequest(
				fmt.Sprintf("http://%s", appURL),
				bytes.NewReader([]byte(payload)),
			)
			resp, err := testState.client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			respBody, err := ioutil.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()

			Expect(respBody).To(HaveLen(payloadSize))
		})
	})
})

func newEchoApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, delay time.Duration, routeServiceUrl string) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, nil, routeServiceUrl)
	app.AddHandler("/", func(w http.ResponseWriter, r *http.Request) {
		defer ginkgo.GinkgoRecover()

		if r.Method == http.MethodPost {
			buf := make([]byte, 4096)

			i := 0
			for {
				n, err := r.Body.Read(buf)
				if n > 0 {
					i++
					_, err = w.Write(buf[:n])
					Expect(err).NotTo(HaveOccurred(), "Encountered unexpected write error")
				} else if err != nil {
					if err != io.EOF {
						Expect(err).NotTo(HaveOccurred(), "Encountered unexpected read error")
					}
					break
				}
			}
		}
	})

	return app
}
