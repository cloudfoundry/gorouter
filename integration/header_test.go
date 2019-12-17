package integration

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"code.cloudfoundry.org/gorouter/config"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Headers", func() {
	var (
		testState *testState

		testAppRoute string
		testApp      *StateTrackingTestApp
	)

	const (
		testHeader      = "Test-Header"
		testHeaderValue = "Value"
	)

	BeforeEach(func() {
		testState = NewTestState()
		testApp = NewUnstartedTestApp(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				defer GinkgoRecover()
				_, err := ioutil.ReadAll(r.Body)
				Expect(err).NotTo(HaveOccurred())
				w.Header().Set(testHeader, testHeaderValue)
				w.WriteHeader(200)
			}))
		testAppRoute = "potato.potato"
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
		testApp.Close()
	})

	Context("Sanity Test", func() {
		BeforeEach(func() {
			testState.StartGorouter()
			testApp.Start()
			testState.register(testApp.Server, testAppRoute)
		})

		It("returns a header that was set by the app", func() {
			req := testState.newRequest(fmt.Sprintf("http://%s", testAppRoute))
			resp, err := testState.client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			_, err = ioutil.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Header.Get(testHeader)).To(Equal(testHeaderValue))
			resp.Body.Close()
		})
	})

	Context("Remove Headers", func() {
		BeforeEach(func() {
			testState.cfg.HTTPRewrite.Responses.RemoveHeaders =
				[]config.HeaderNameValue{
					{
						Name: testHeader,
					},
				}

			testState.StartGorouter()
			testApp.Start()
			testState.register(testApp.Server, testAppRoute)
		})

		It("removes the header specified in the config", func() {
			req := testState.newRequest(fmt.Sprintf("http://%s", testAppRoute))
			resp, err := testState.client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			_, err = ioutil.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Header.Get(testHeader)).To(BeEmpty())
			resp.Body.Close()
		})
	})

	Context("Add Headers", func() {
		const (
			newHeader      = "New-Header"
			newHeaderValue = "newValue"
		)

		BeforeEach(func() {
			testState.cfg.HTTPRewrite.Responses.AddHeadersIfNotPresent =
				[]config.HeaderNameValue{
					{
						Name:  newHeader,
						Value: newHeaderValue,
					},
				}

			testState.StartGorouter()
			testApp.Start()
			testState.register(testApp.Server, testAppRoute)
		})

		It("adds the header specified in the config", func() {
			req := testState.newRequest(fmt.Sprintf("http://%s", testAppRoute))
			resp, err := testState.client.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			_, err = ioutil.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Header.Get(newHeader)).To(Equal(newHeaderValue))
			resp.Body.Close()
		})
	})
})

