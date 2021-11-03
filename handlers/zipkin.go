package handlers

import (
	"encoding/hex"
	"net/http"

	"github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/model"
	"github.com/openzipkin/zipkin-go/propagation/b3"
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

	tracer, err := zipkin.NewTracer(nil, zipkin.WithNoopTracer(!z.zipkinEnabled))
	if err != nil {
		z.logger.Info("failed-to-create-tracer", zap.Error(err))
		return
	}

	sp, _ := tracer.StartSpanFromContext(
		r.Context(), r.URL.Scheme+"/"+r.Method, zipkin.Kind(model.Client),
	)

	b3.InjectHTTP(r)(sp.Context())
}

func generateSpanID() (string, error) {
	randBytes, err := secure.RandomBytes(8)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(randBytes), nil
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
