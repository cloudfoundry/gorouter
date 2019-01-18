package handlers

import (
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/uber-go/zap"
	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/logger"

	"code.cloudfoundry.org/gorouter/common/secure"
)

const (
	B3Header             = "b3"
	B3TraceIdHeader      = "X-B3-TraceId"
	B3SpanIdHeader       = "X-B3-SpanId"
	B3ParentSpanIdHeader = "X-B3-ParentSpanId"
	B3SampledHeader      = "X-B3-Sampled"
	B3FlagsHeader        = "X-B3-Flags"
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

	existingContext := r.Header.Get(B3Header)
	if existingContext != "" {
		z.logger.Debug("b3-header-exists",
			zap.String("B3Header", existingContext),
		)

		return
	}

	existingTraceID := r.Header.Get(B3TraceIdHeader)
	existingSpanID := r.Header.Get(B3SpanIdHeader)
	if existingTraceID == "" || existingSpanID == "" {
		traceID, err := generateSpanID()
		if err != nil {
			z.logger.Info("failed-to-create-b3-trace-id", zap.Error(err))
			return
		}

		r.Header.Set(B3TraceIdHeader, traceID)
		r.Header.Set(B3SpanIdHeader, traceID)
		r.Header.Set(B3Header, traceID+"-"+traceID)
	} else {
		r.Header.Set(B3Header, BuildB3SingleHeader(
			existingTraceID,
			existingSpanID,
			r.Header.Get(B3SampledHeader),
			r.Header.Get(B3FlagsHeader),
			r.Header.Get(B3ParentSpanIdHeader),
		))

		z.logger.Debug("b3-trace-id-span-id-header-exists",
			zap.String("B3TraceIdHeader", existingTraceID),
			zap.String("B3SpanIdHeader", existingSpanID),
		)
	}
}

func generateSpanID() (string, error) {
	randBytes, err := secure.RandomBytes(8)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(randBytes), nil
}

// BuildB3SingleHeader assembles the B3 single header based on existing trace
// values
func BuildB3SingleHeader(traceID, spanID, sampling, flags, parentSpanID string) string {
	if traceID == "" || spanID == "" {
		return ""
	}

	if sampling == "" && flags == "" {
		return traceID + "-" + spanID
	}

	samplingBit := "0"
	if flags == "1" {
		samplingBit = "d"
	} else if s, err := strconv.ParseBool(sampling); err == nil {
		if s {
			samplingBit = "1"
		}
	} else {
		return traceID + "-" + spanID
	}

	if parentSpanID == "" {
		return traceID + "-" + spanID + "-" + samplingBit
	}

	return traceID + "-" + spanID + "-" + samplingBit + "-" + parentSpanID
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

	if !contains(headersToLog, B3Header) {
		headersToLog = append(headersToLog, B3Header)
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
