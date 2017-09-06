package round_tripper

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"github.com/uber-go/zap"
)

type ErrorHandler struct {
	MetricReporter metrics.CombinedReporter
}

func (eh *ErrorHandler) HandleError(logger logger.Logger, responseWriter utils.ProxyResponseWriter, err error) {
	responseWriter.Header().Set(router_http.CfRouterError, "endpoint_failure")

	switch err.(type) {
	case tls.RecordHeaderError:
		http.Error(responseWriter, SSLHandshakeMessage, 525)
	case x509.HostnameError:
		http.Error(responseWriter, HostnameErrorMessage, http.StatusServiceUnavailable)
	case x509.UnknownAuthorityError:
		http.Error(responseWriter, InvalidCertificateMessage, 526)
	default:
		if typedErr, ok := err.(*net.OpError); ok && typedErr.Op == "remote error" && typedErr.Err.Error() == "tls: bad certificate" {
			http.Error(responseWriter, SSLCertRequiredMessage, 496)
		} else {
			http.Error(responseWriter, BadGatewayMessage, http.StatusBadGateway)
			eh.MetricReporter.CaptureBadGateway()
		}
	}

	logger.Error("endpoint-failed", zap.Error(err))
	responseWriter.Header().Del("Connection")
	responseWriter.Done()
}
