package handlers

import (
	"fmt"
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
	traceID, spanID := requestInfo.ProvideTraceInfo()
	guid := s.buildVcapRequestID(traceID, spanID)

	if err == nil {
		r.Header.Set(VcapRequestIdHeader, guid)
		s.logger.Debug("vcap-request-id-header-set", zap.String("VcapRequestIdHeader", guid))
	} else {
		s.logger.Error("failed-to-set-vcap-request-id-header", zap.Error(err))
	}

	next(rw, r)
}

func (s *setVcapRequestIdHeader) buildVcapRequestID(traceID string, spanID string) string {
	return fmt.Sprintf("%s-%s", traceID, spanID)
}
