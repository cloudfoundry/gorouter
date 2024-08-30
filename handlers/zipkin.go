package handlers

import (
	"log/slog"
	"net/http"

	log "code.cloudfoundry.org/gorouter/logger"
	"github.com/openzipkin/zipkin-go/propagation/b3"
	"github.com/urfave/negroni/v3"
)

// Zipkin is a handler that sets Zipkin headers on requests
type Zipkin struct {
	zipkinEnabled bool
	logger        *slog.Logger
}

var _ negroni.Handler = new(Zipkin)

// NewZipkin creates a new handler that sets Zipkin headers on requests
func NewZipkin(enabled bool, logger *slog.Logger) *Zipkin {
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
		logger.Error("failed-to-get-request-info", log.ErrAttr(err))
		return
	}

	existingContext := r.Header.Get(b3.Context)
	if existingContext != "" {
		logger.Debug("b3-header-exists",
			slog.String("b3", existingContext),
		)

		sc, err := b3.ParseSingleHeader(existingContext)
		if err != nil {
			logger.Error("failed-to-parse-single-header", log.ErrAttr(err))
		} else {
			err = requestInfo.SetTraceInfo(sc.TraceID.String(), sc.ID.String())
			if err != nil {
				logger.Error("failed-to-set-trace-info", log.ErrAttr(err))
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
			logger.Info("failed-to-parse-b3-trace-id", log.ErrAttr(err))
			return
		}
		r.Header.Set(b3.Context, b3.BuildSingleHeader(*sc))

		logger.Debug("b3-trace-id-span-id-header-exists",
			slog.String("trace-id", existingTraceID),
			slog.String("span-id", existingSpanID),
		)

		err = requestInfo.SetTraceInfo(sc.TraceID.String(), sc.ID.String())
		if err != nil {
			logger.Error("failed-to-set-trace-info", log.ErrAttr(err))
		} else {
			return
		}
	}

	traceInfo, err := requestInfo.ProvideTraceInfo()
	if err != nil {
		logger.Error("failed-to-get-trace-info", log.ErrAttr(err))
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
