package handlers

import (
	"net/http"

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

	logger := LoggerWithTraceInfo(z.logger, r)

	if !z.zipkinEnabled {
		return
	}

	requestInfo, err := ContextRequestInfo(r)
	if err != nil {
		logger.Error("failed-to-get-request-info", zap.Error(err))
		return
	}

	existingContext := r.Header.Get(b3.Context)
	if existingContext != "" {
		logger.Debug("b3-header-exists",
			zap.String("b3", existingContext),
		)

		sc, err := b3.ParseSingleHeader(existingContext)
		if err != nil {
			logger.Error("failed-to-parse-single-header", zap.Error(err))
		} else {
			err = requestInfo.SetTraceInfo(sc.TraceID.String(), sc.ID.String())
			if err != nil {
				logger.Error("failed-to-set-trace-info", zap.Error(err))
			} else {
				return
			}
		}
	}

	existingTraceID := r.Header.Get(b3.TraceID)
	existingSpanID := r.Header.Get(b3.SpanID)
	if existingTraceID != "" && existingSpanID != "" {
		sc, err := b3.ParseHeaders(
			existingTraceID,
			existingSpanID,
			r.Header.Get(b3.ParentSpanID),
			r.Header.Get(b3.Sampled),
			r.Header.Get(b3.Flags),
		)
		if err != nil {
			logger.Info("failed-to-parse-b3-trace-id", zap.Error(err))
			return
		}
		r.Header.Set(b3.Context, b3.BuildSingleHeader(*sc))

		logger.Debug("b3-trace-id-span-id-header-exists",
			zap.String("trace-id", existingTraceID),
			zap.String("span-id", existingSpanID),
		)

		err = requestInfo.SetTraceInfo(sc.TraceID.String(), sc.ID.String())
		if err != nil {
			logger.Error("failed-to-set-trace-info", zap.Error(err))
		} else {
			return
		}
	}

	traceInfo, err := requestInfo.ProvideTraceInfo()
	if err != nil {
		logger.Error("failed-to-get-trace-info", zap.Error(err))
		return
	}

	r.Header.Set(b3.TraceID, traceInfo.TraceID)
	r.Header.Set(b3.SpanID, traceInfo.SpanID)
	r.Header.Set(b3.Context, traceInfo.TraceID+"-"+traceInfo.SpanID)
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
