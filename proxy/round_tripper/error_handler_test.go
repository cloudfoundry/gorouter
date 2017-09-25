package round_tripper_test

import (
	"errors"
	"net/http/httptest"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy/round_tripper"
	"code.cloudfoundry.org/gorouter/proxy/utils"

	"code.cloudfoundry.org/gorouter/proxy/fails"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("HandleError", func() {
	var (
		metricReporter   *fakes.FakeCombinedReporter
		errorHandler     *round_tripper.ErrorHandler
		responseWriter   utils.ProxyResponseWriter
		responseRecorder *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		metricReporter = new(fakes.FakeCombinedReporter)
		errorHandler = &round_tripper.ErrorHandler{
			MetricReporter: metricReporter,
			ErrorSpecs: []round_tripper.ErrorSpec{
				{
					Code:    418,
					Message: "teapot",
					Classifier: fails.ClassifierFunc(func(err error) bool {
						return err.Error() == "i'm a teapot"
					}),
				},
				{
					Code:    419,
					Message: "you say tomato",
					Classifier: fails.ClassifierFunc(func(err error) bool {
						return err.Error() == "i'm a tomato"
					}),
				},
			},
		}
		responseRecorder = httptest.NewRecorder()
		responseWriter = utils.NewProxyResponseWriter(responseRecorder)
	})

	It("Sets a header to describe the endpoint_failure", func() {
		errorHandler.HandleError(responseWriter, errors.New("potato"))
		Expect(responseWriter.Header().Get(router_http.CfRouterError)).To(Equal("endpoint_failure"))
	})

	Context("when the error does not match any of the classifiers", func() {
		It("sets the http response code to 502", func() {
			errorHandler.HandleError(responseWriter, errors.New("potato"))
			Expect(responseWriter.Status()).To(Equal(502))
		})

		It("emits a BadGateway metric", func() {
			errorHandler.HandleError(responseWriter, errors.New("potato"))
			Expect(metricReporter.CaptureBadGatewayCallCount()).To(Equal(1))
		})
	})

	Context("when the error does match one of the classifiers", func() {
		It("sets the http response code and message appropriately", func() {
			errorHandler.HandleError(responseWriter, errors.New("i'm a tomato"))
			Expect(responseWriter.Status()).To(Equal(419))
			Expect(responseRecorder.Body.String()).To(Equal("you say tomato\n"))
		})

		It("does not emit a metric", func() {
			errorHandler.HandleError(responseWriter, errors.New("i'm a tomato"))
			Expect(metricReporter.CaptureBadGatewayCallCount()).To(Equal(0))
		})
	})

	It("removes any headers named 'Connection'", func() {
		responseWriter.Header().Add("Connection", "foo")
		errorHandler.HandleError(responseWriter, errors.New("potato"))
		Expect(responseWriter.Header().Get("Connection")).To(BeEmpty())
	})

	It("calls Done on the responseWriter, preventing further writes from going through", func() {
		errorHandler.HandleError(responseWriter, errors.New("potato"))
		nBytesWritten, err := responseWriter.Write([]byte("foo"))
		Expect(err).NotTo(HaveOccurred())
		Expect(nBytesWritten).To(Equal(0))
	})
})
