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
}

var _ negroni.Handler = new(Zipkin)

// NewZipkin creates a new handler that sets Zipkin headers on requests
func NewZipkin(enabled bool, logger logger.Logger) *Zipkin {
	return &Zipkin{
		zipkinEnabled: enabled,
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

// HeadersToLog specifies the headers which should be logged if Zipkin headers
// are enabled
func (z *Zipkin) HeadersToLog() []string {
	if !z.zipkinEnabled {
		return []string{}
	}

	return []string{
		B3TraceIdHeader,
		B3SpanIdHeader,
		B3ParentSpanIdHeader,
		B3Header,
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
