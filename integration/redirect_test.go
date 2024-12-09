package integration

import (
	"fmt"
	"io"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Headers", func() {
	var (
		testState *testState

		testAppRoute string
		testApp      *StateTrackingTestApp
	)

	BeforeEach(func() {
		testState = NewTestState()
		testApp = NewUnstartedTestApp(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				defer GinkgoRecover()
				_, err := io.ReadAll(r.Body)
				Expect(err).NotTo(HaveOccurred())
				w.Header().Set("Location", "redirect.com")
				w.WriteHeader(http.StatusFound)
			}))
		testAppRoute = "potato.potato"
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
		testApp.Close()
	})

	Context("When an app returns a 3xx-redirect", func() {
		BeforeEach(func() {
			testState.StartGorouterOrFail()
			testApp.Start()
			testState.register(testApp.Server, testAppRoute)
		})

		It("does not follow the redirect and instead forwards it to the client", func() {
			req := testState.newGetRequest(fmt.Sprintf("http://%s", testAppRoute))

			// this makes the test client NOT follow redirects, so that we can
			// test that the return code is indeed 3xx
			testState.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}

			resp, err := testState.client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusFound))

			_, err = io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
		})
	})

})
