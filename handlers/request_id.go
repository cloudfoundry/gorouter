package handlers

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

const (
	VcapRequestIdHeader = "X-Vcap-Request-Id"
)

type setVcapRequestIdHeader struct {
	logger logger.Logger
}

func NewVcapRequestIdHeader(logger logger.Logger) negroni.Handler {
	return &setVcapRequestIdHeader{
		logger: logger,
	}
}

func (s *setVcapRequestIdHeader) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	// The X-Vcap-Request-Id must be set before the request is passed into the
	// dropsonde InstrumentedHandler

	requestInfo, err := ContextRequestInfo(r)
	if err != nil {
		s.logger.Error("failed-to-get-request-info", zap.Error(err))
		return
	}

	logger := LoggerWithTraceInfo(s.logger, r)

	traceInfo, err := requestInfo.ProvideTraceInfo()
	if err != nil {
		logger.Error("failed-to-get-trace-info", zap.Error(err))
		return
	}

	r.Header.Set(VcapRequestIdHeader, traceInfo.UUID)
	logger.Debug("vcap-request-id-header-set", zap.String("VcapRequestIdHeader", traceInfo.UUID))

	next(rw, r)
}
