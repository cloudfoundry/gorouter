package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("W3C tracing headers", func() {
	const (
		hostname = "w3c-tracing-app.cloudfoundry.org"
	)

	var (
		testState *testState
		testApp   *httptest.Server

		appReceivedHeaders chan http.Header

		doRequest func(http.Header)
	)

	BeforeEach(func() {
		testState = NewTestState()
		testState.cfg.Tracing.EnableW3C = true

		appReceivedHeaders = make(chan http.Header, 1)

		doRequest = func(headers http.Header) {
			req := testState.newRequest(fmt.Sprintf("http://%s", hostname))

			for headerName, headerVals := range headers {
				for _, headerVal := range headerVals {
					req.Header.Set(headerName, headerVal)
				}
			}

			resp, err := testState.client.Do(req)

			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))

			resp.Body.Close()
		}
	})

	JustBeforeEach(func() {
		testState.StartGorouterOrFail()

		testApp = httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				appReceivedHeaders <- r.Header
				w.WriteHeader(200)
			}),
		)

		testState.register(testApp, hostname)
	})

	AfterEach(func() {
		testApp.Close()

		if testState != nil {
			testState.StopAndCleanup()
		}
	})

	Context("when W3C is enabled without a tenant ID", func() {
		BeforeEach(func() {
			testState.cfg.Tracing.EnableW3C = true
		})

		It("generates new trace headers when the request has none", func() {
			doRequest(http.Header{})

			var expectedBackendHeader http.Header
			Eventually(appReceivedHeaders, "3s").Should(Receive(&expectedBackendHeader))

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Traceparent",
				ConsistOf(MatchRegexp("^00-[a-f0-9]{32}-[a-f0-9]{16}-01$"))),
			)

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Tracestate",
				ConsistOf(MatchRegexp("gorouter=[a-f0-9]{16}"))),
			)
		})

		It("updates existing trace headers when the request has them", func() {
			doRequest(http.Header{
				"traceparent": []string{
					"00-11111111111111111111111111111111-9999999999999999-01",
				},
				"tracestate": []string{
					"congo=12345678",
				},
			})

			var expectedBackendHeader http.Header
			Eventually(appReceivedHeaders, "3s").Should(Receive(&expectedBackendHeader))

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Traceparent",
				ConsistOf(MatchRegexp("^00-[1]{32}-[a-f0-9]{16}-01$"))),
			)

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Tracestate",
				ConsistOf(MatchRegexp("gorouter=[a-f0-9]{16},congo=12345678"))),
			)
		})

		It("updates existing trace headers with the same tenant ID", func() {
			doRequest(http.Header{
				"traceparent": []string{
					"00-11111111111111111111111111111111-9999999999999999-01",
				},
				"tracestate": []string{
					"congo=12345678,gorouter=abcdefg,rojo=xyz1234",
				},
			})

			var expectedBackendHeader http.Header
			Eventually(appReceivedHeaders, "3s").Should(Receive(&expectedBackendHeader))

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Traceparent",
				ConsistOf(MatchRegexp("^00-[1]{32}-[a-f0-9]{16}-01$"))),
			)

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Tracestate",
				ConsistOf(MatchRegexp("gorouter=[a-f0-9]{16},congo=12345678,rojo=xyz1234"))),
			)
		})
	})

	Context("when W3C is enabled with a tenant ID", func() {
		BeforeEach(func() {

			testState.cfg.Tracing.EnableW3C = true
			testState.cfg.Tracing.W3CTenantID = "tid"
		})

		It("generates new trace headers when the request has none", func() {
			doRequest(http.Header{})

			var expectedBackendHeader http.Header
			Eventually(appReceivedHeaders, "3s").Should(Receive(&expectedBackendHeader))

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Traceparent",
				ConsistOf(MatchRegexp("^00-[a-f0-9]{32}-[a-f0-9]{16}-01$"))),
			)

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Tracestate",
				ConsistOf(MatchRegexp("tid@gorouter=[a-f0-9]{16}"))),
			)
		})

		It("updates existing trace headers when the request has them", func() {
			doRequest(http.Header{
				"traceparent": []string{
					"00-11111111111111111111111111111111-9999999999999999-01",
				},
				"tracestate": []string{
					"congo=12345678",
				},
			})

			var expectedBackendHeader http.Header
			Eventually(appReceivedHeaders, "3s").Should(Receive(&expectedBackendHeader))

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Traceparent",
				ConsistOf(MatchRegexp("^00-[1]{32}-[a-f0-9]{16}-01$"))),
			)

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Tracestate",
				ConsistOf(MatchRegexp("tid@gorouter=[a-f0-9]{16},congo=12345678"))),
			)
		})

		It("updates existing trace headers with the same tenant ID", func() {
			doRequest(http.Header{
				"traceparent": []string{
					"00-11111111111111111111111111111111-9999999999999999-01",
				},
				"tracestate": []string{
					"congo=12345678,tid@gorouter=abcdefg,rojo=xyz1234",
				},
			})

			var expectedBackendHeader http.Header
			Eventually(appReceivedHeaders, "3s").Should(Receive(&expectedBackendHeader))

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Traceparent",
				ConsistOf(MatchRegexp("^00-[1]{32}-[a-f0-9]{16}-01$"))),
			)

			Expect(expectedBackendHeader).To(HaveKeyWithValue("Tracestate",
				ConsistOf(MatchRegexp("tid@gorouter=[a-f0-9]{16},congo=12345678,rojo=xyz1234"))),
			)
		})
	})
})
