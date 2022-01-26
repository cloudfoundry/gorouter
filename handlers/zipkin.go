package handlers

import (
	"net/http"

	"github.com/openzipkin/zipkin-go/idgenerator"
	"github.com/openzipkin/zipkin-go/propagation/b3"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/logger"
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

	existingContext := r.Header.Get(b3.Context)
	if existingContext != "" {
		z.logger.Debug("b3-header-exists",
			zap.String("b3", existingContext),
		)

		return
	}

	existingTraceID := r.Header.Get(b3.TraceID)
	existingSpanID := r.Header.Get(b3.SpanID)
	if existingTraceID == "" || existingSpanID == "" {
		trace := idgenerator.NewRandom128().TraceID()
		span := idgenerator.NewRandom128().SpanID(trace).String()

		r.Header.Set(b3.TraceID, trace.String())
		r.Header.Set(b3.SpanID, span)
		r.Header.Set(b3.Context,  trace.String()+"-"+span)
	} else {
		sc, err := b3.ParseHeaders(
			existingTraceID,
			existingSpanID,
			r.Header.Get(b3.ParentSpanID),
			r.Header.Get(b3.Sampled),
			r.Header.Get(b3.Flags),
		)
		if err != nil {
			z.logger.Info("failed-to-parse-b3-trace-id", zap.Error(err))
			return
		}
		r.Header.Set(b3.Context, b3.BuildSingleHeader(*sc))

		z.logger.Debug("b3-trace-id-span-id-header-exists",
			zap.String("traceID", existingTraceID),
			zap.String("spanID", existingSpanID),
		)
	}
}

// HeadersToLog specifies the headers which should be logged if Zipkin headers
// are enabled
func (z *Zipkin) HeadersToLog() []string {
	if !z.zipkinEnabled {
		return []string{}
	}

	return []string{
		b3.TraceID,
		b3.SpanID,
		b3.ParentSpanID,
		b3.Context,
	}
}
