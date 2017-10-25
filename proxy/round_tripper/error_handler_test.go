package round_tripper_test

import (
	"errors"
	"net"
	"net/http/httptest"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy/round_tripper"
	"code.cloudfoundry.org/gorouter/proxy/utils"

	"crypto/tls"

	"crypto/x509"

	"context"

	"code.cloudfoundry.org/gorouter/metrics"
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
		errorHandled     bool
	)

	BeforeEach(func() {
		metricReporter = new(fakes.FakeCombinedReporter)
		errorHandled = false
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
					HandleError: func(_ metrics.ProxyReporter) {
						errorHandled = true
					},
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

		It("calls the handleError callback if it exists", func() {
			firstResponseWriter := utils.NewProxyResponseWriter(httptest.NewRecorder())
			errorHandler.HandleError(firstResponseWriter, errors.New("i'm a teapot"))
			Expect(errorHandled).To(BeFalse())

			errorHandler.HandleError(responseWriter, errors.New("i'm a tomato"))
			Expect(responseWriter.Status()).To(Equal(419))
			Expect(errorHandled).To(BeTrue())
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

	Context("DefaultErrorSpecs", func() {
		var err error

		BeforeEach(func() {
			errorHandler = &round_tripper.ErrorHandler{
				MetricReporter: metricReporter,
				ErrorSpecs:     round_tripper.DefaultErrorSpecs,
			}
		})

		Context("HostnameMismatch", func() {
			BeforeEach(func() {
				err = x509.HostnameError{Host: "the wrong one"}
				errorHandler.HandleError(responseWriter, err)
			})

			It("Has a 503 Status Code", func() {
				Expect(responseWriter.Status()).To(Equal(503))
			})

			It("Emits a backend_invalid_id metric", func() {
				Expect(metricReporter.CaptureBackendInvalidIDCallCount()).To(Equal(1))
			})
		})

		Context("Untrusted Cert", func() {
			BeforeEach(func() {
				err = x509.UnknownAuthorityError{}
				errorHandler.HandleError(responseWriter, err)
			})

			It("Has a 526 Status Code", func() {
				Expect(responseWriter.Status()).To(Equal(526))
			})

			It("Emits a backend_invalid_tls_cert metric", func() {
				Expect(metricReporter.CaptureBackendInvalidTLSCertCallCount()).To(Equal(1))
			})
		})

		Context("Attempted TLS with non-TLS backend error", func() {
			BeforeEach(func() {
				err = tls.RecordHeaderError{Msg: "bad handshake"}
				errorHandler.HandleError(responseWriter, err)
			})

			It("Has a 525 Status Code", func() {
				Expect(responseWriter.Status()).To(Equal(525))
			})

			It("Emits a backend_tls_handshake_failed metric", func() {
				Expect(metricReporter.CaptureBackendTLSHandshakeFailedCallCount()).To(Equal(1))
			})
		})

		Context("Remote handshake failure", func() {
			BeforeEach(func() {
				err = &net.OpError{Op: "remote error", Err: errors.New("tls: handshake failure")}
				errorHandler.HandleError(responseWriter, err)
			})

			It("Has a 525 Status Code", func() {
				Expect(responseWriter.Status()).To(Equal(525))
			})

			It("Emits a backend_tls_handshake_failed metric", func() {
				Expect(metricReporter.CaptureBackendTLSHandshakeFailedCallCount()).To(Equal(1))
			})
		})

		Context("Context Cancelled Error", func() {
			BeforeEach(func() {
				err = context.Canceled
				errorHandler.HandleError(responseWriter, err)
			})

			It("Has a 499 Status Code", func() {
				Expect(responseWriter.Status()).To(Equal(499))
			})
		})
	})
})
