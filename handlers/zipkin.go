package handlers

import (
	"encoding/hex"
	"net/http"

	"github.com/uber-go/zap"
	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/logger"

	"code.cloudfoundry.org/gorouter/common/secure"
)

const (
	B3TraceIdHeader      = "X-B3-TraceId"
	B3SpanIdHeader       = "X-B3-SpanId"
	B3ParentSpanIdHeader = "X-B3-ParentSpanId"
)

// Zipkin is a handler that sets Zipkin headers on requests
type Zipkin struct {
	zipkinEnabled bool
	logger        logger.Logger
	headersToLog  []string // Shared state with proxy for access logs
}

var _ negroni.Handler = new(Zipkin)

// NewZipkin creates a new handler that sets Zipkin headers on requests
func NewZipkin(enabled bool, headersToLog []string, logger logger.Logger) *Zipkin {
	return &Zipkin{
		zipkinEnabled: enabled,
		headersToLog:  headersToLog,
		logger:        logger,
	}
}

func (z *Zipkin) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	defer next(rw, r)
	if !z.zipkinEnabled {
		return
	}

	existingTraceId := r.Header.Get(B3TraceIdHeader)
	existingSpanId := r.Header.Get(B3SpanIdHeader)

	if existingTraceId == "" || existingSpanId == "" {
		randBytes, err := secure.RandomBytes(8)
		if err != nil {
			z.logger.Info("failed-to-create-b3-trace-id", zap.Error(err))
			return
		}

		id := hex.EncodeToString(randBytes)
		r.Header.Set(B3TraceIdHeader, id)
		r.Header.Set(B3SpanIdHeader, r.Header.Get(B3TraceIdHeader))
	} else {
		z.logger.Debug("b3-trace-id-span-id-header-exists",
			zap.String("B3TraceIdHeader", existingTraceId),
			zap.String("B3SpanIdHeader", existingSpanId),
		)
	}
	return
}

// HeadersToLog returns headers that should be logged in the access logs and
// includes Zipkin headers in this set if necessary
func (z *Zipkin) HeadersToLog() []string {
	if !z.zipkinEnabled {
		return z.headersToLog
	}
	headersToLog := z.headersToLog
	if !contains(headersToLog, B3TraceIdHeader) {
		headersToLog = append(headersToLog, B3TraceIdHeader)
	}

	if !contains(headersToLog, B3SpanIdHeader) {
		headersToLog = append(headersToLog, B3SpanIdHeader)
	}

	if !contains(headersToLog, B3ParentSpanIdHeader) {
		headersToLog = append(headersToLog, B3ParentSpanIdHeader)
	}
	return headersToLog
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
