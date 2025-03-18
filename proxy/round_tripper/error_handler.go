package round_tripper

import (
	"fmt"
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
	HandleError func(reporter metrics.MetricReporter)
}

func handleHostnameMismatch(reporter metrics.MetricReporter) {
	reporter.CaptureBackendInvalidID()
}

func handleSSLHandshake(reporter metrics.MetricReporter) {
	reporter.CaptureBackendTLSHandshakeFailed()
}

func handleUntrustedCert(reporter metrics.MetricReporter) {
	reporter.CaptureBackendInvalidTLSCert()
}

var DefaultErrorSpecs = []ErrorSpec{
	{fails.AttemptedTLSWithNonTLSBackend, SSLHandshakeMessage, 525, handleSSLHandshake},
	{fails.HostnameMismatch, HostnameErrorMessage, http.StatusServiceUnavailable, handleHostnameMismatch},
	{fails.UntrustedCert, InvalidCertificateMessage, 526, handleUntrustedCert},
	{fails.RemoteFailedCertCheck, SSLCertRequiredMessage, 496, nil},
	{fails.ContextCancelled, ContextCancelledMessage, 499, nil},
	{fails.RemoteHandshakeFailure, SSLHandshakeMessage, 525, handleSSLHandshake},
}

type ErrorHandler struct {
	MetricReporter metrics.MetricReporter
	ErrorSpecs     []ErrorSpec
}

func (eh *ErrorHandler) HandleError(responseWriter utils.ProxyResponseWriter, err error) {
	msg := "endpoint_failure"
	if err != nil {
		msg = fmt.Sprintf("%s (%s)", msg, err)
	}

	responseWriter.Header().Set(router_http.CfRouterError, msg)

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
