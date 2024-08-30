package handlers

import (
	"fmt"
	"log/slog"
	"net/http"

	log "code.cloudfoundry.org/gorouter/logger"
	"github.com/urfave/negroni/v3"
)

const (
	W3CTraceparentHeader = "traceparent"
	W3CTracestateHeader  = "tracestate"

	W3CVendorID = "gorouter"
)

// W3C is a handler that sets W3C headers on requests
type W3C struct {
	w3cEnabled  bool
	w3cTenantID string
	logger      *slog.Logger
}

var _ negroni.Handler = new(W3C)

// NewW3C creates a new handler that sets W3C headers on requests
func NewW3C(enabled bool, tenantID string, logger *slog.Logger) *W3C {
	return &W3C{
		w3cEnabled:  enabled,
		w3cTenantID: tenantID,
		logger:      logger,
	}
}

func (m *W3C) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	defer next(rw, r)

	if !m.w3cEnabled {
		return
	}

	requestInfo, err := ContextRequestInfo(r)
	if err != nil {
		m.logger.Error("failed-to-get-request-info", log.ErrAttr(err))
		return
	}

	logger := LoggerWithTraceInfo(m.logger, r)

	prevTraceparent := ParseW3CTraceparent(r.Header.Get(W3CTraceparentHeader))

	if prevTraceparent == nil {
		// If we cannot parse an existing traceparent header
		// or if there is no traceparent header
		// then we should use trace ID and span ID saved in the request context
		m.ServeNewTraceparent(rw, r, requestInfo, logger)
	} else {
		m.ServeUpdatedTraceparent(rw, r, requestInfo, *prevTraceparent, logger)
	}
}

func (m *W3C) ServeNewTraceparent(rw http.ResponseWriter, r *http.Request, requestInfo *RequestInfo, logger *slog.Logger) {
	traceparent, err := NewW3CTraceparent(requestInfo)
	if err != nil {
		logger.Error("failed-to-create-w3c-traceparent", log.ErrAttr(err))
		return
	}

	tracestate := NewW3CTracestate(m.w3cTenantID, traceparent.ParentID)

	r.Header.Set(W3CTraceparentHeader, traceparent.String())
	r.Header.Set(W3CTracestateHeader, tracestate.String())
}

func (m *W3C) ServeUpdatedTraceparent(
	rw http.ResponseWriter,
	r *http.Request,
	requestInfo *RequestInfo,
	prevTraceparent W3CTraceparent,
	logger *slog.Logger,
) {
	traceparent, err := prevTraceparent.Next()
	if err != nil {
		logger.Info("failed-to-generate-next-w3c-traceparent", log.ErrAttr(err))
		return
	}

	if requestInfo.TraceInfo.TraceID == "" {
		requestInfo.SetTraceInfo(fmt.Sprintf("%x", traceparent.TraceID), fmt.Sprintf("%x", traceparent.ParentID))
	}

	tracestate := ParseW3CTracestate(r.Header.Get(W3CTracestateHeader))
	tracestate = tracestate.Next(m.w3cTenantID, traceparent.ParentID)

	r.Header.Set(W3CTraceparentHeader, traceparent.String())
	r.Header.Set(W3CTracestateHeader, tracestate.String())
}

// HeadersToLog specifies the headers which should be logged if W3C headers are
// enabled
func (m *W3C) HeadersToLog() []string {
	if !m.w3cEnabled {
		return []string{}
	}

	return []string{
		W3CTraceparentHeader,
		W3CTracestateHeader,
	}
}
