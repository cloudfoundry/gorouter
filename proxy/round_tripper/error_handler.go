package round_tripper

import (
	"net/http"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/proxy/fails"
	"code.cloudfoundry.org/gorouter/proxy/utils"
)

type ErrorSpec struct {
	Classifier  fails.Classifier
	Message     string
	Code        int
	HandleError func(reporter metrics.CombinedReporter)
}

func handleHostnameMismatch(reporter metrics.CombinedReporter) {
	reporter.CaptureBackendInvalidID()
}

func handleSSLHandshake(reporter metrics.CombinedReporter) {
	reporter.CaptureBackendTLSHandshakeFailed()
}

var DefaultErrorSpecs = []ErrorSpec{
	{fails.AttemptedTLSWithNonTLSBackend, SSLHandshakeMessage, 525, handleSSLHandshake},
	{fails.HostnameMismatch, HostnameErrorMessage, http.StatusServiceUnavailable, handleHostnameMismatch},
	{fails.UntrustedCert, InvalidCertificateMessage, 526, nil},
	{fails.RemoteFailedCertCheck, SSLCertRequiredMessage, 496, nil},
}

type ErrorHandler struct {
	MetricReporter metrics.CombinedReporter
	ErrorSpecs     []ErrorSpec
}

func (eh *ErrorHandler) HandleError(responseWriter utils.ProxyResponseWriter, err error) {
	responseWriter.Header().Set(router_http.CfRouterError, "endpoint_failure")

	eh.writeErrorCode(err, responseWriter)
	responseWriter.Header().Del("Connection")
	responseWriter.Done()
}

func (eh *ErrorHandler) writeErrorCode(err error, responseWriter http.ResponseWriter) {
	for _, spec := range eh.ErrorSpecs {
		if spec.Classifier.Classify(err) {
			if spec.HandleError != nil {
				spec.HandleError(eh.MetricReporter)
			}
			http.Error(responseWriter, spec.Message, spec.Code)
			return
		}
	}

	// default case
	http.Error(responseWriter, BadGatewayMessage, http.StatusBadGateway)
	eh.MetricReporter.CaptureBadGateway()
}
