package handlers

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"time"

	"github.com/openzipkin/zipkin-go/idgenerator"
	zipkinmodel "github.com/openzipkin/zipkin-go/model"
	"github.com/openzipkin/zipkin-go/propagation/b3"
	zipkinhttp "github.com/openzipkin/zipkin-go/reporter/http"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/proxy/utils"
)

// Zipkin is a handler that sets Zipkin headers on requests
type Zipkin struct {
	zipkinEnabled   bool
	collectorConfig config.ZipkinCollectorConfig
	logger          logger.Logger
}

var _ negroni.Handler = new(Zipkin)

// NewZipkin creates a new handler that sets Zipkin headers on requests
func NewZipkin(enabled bool, collectorConfig config.ZipkinCollectorConfig, logger logger.Logger) *Zipkin {
	return &Zipkin{
		zipkinEnabled:   enabled,
		collectorConfig: collectorConfig,
		logger:          logger,
	}
}

func (z *Zipkin) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if !z.zipkinEnabled {
		next(rw, r)
		return
	}

	startTime := time.Now()
	annotations := []zipkinmodel.Annotation{{Timestamp: startTime, Value: "sr"}}
	proxyWriter := rw.(utils.ProxyResponseWriter)

	existingContext := r.Header.Get(b3.Context)
	if existingContext != "" {
		z.logger.Debug("b3-header-exists",
			zap.String("b3", existingContext),
		)

		// TODO: parse single header

		next(rw, r)
		return
	}

	existingTraceID := r.Header.Get(b3.TraceID)
	existingSpanID := r.Header.Get(b3.SpanID)
	if existingTraceID == "" || existingSpanID == "" {
		trace := idgenerator.NewRandom128().TraceID()
		span := idgenerator.NewRandom128().SpanID(trace).String()

		r.Header.Set(b3.TraceID, trace.String())
		r.Header.Set(b3.SpanID, span)
		r.Header.Set(b3.Context, trace.String()+"-"+span)
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

			next(rw, r)
			return
		}
		r.Header.Set(b3.Context, b3.BuildSingleHeader(*sc))

		z.logger.Debug("b3-trace-id-span-id-header-exists",
			zap.String("traceID", existingTraceID),
			zap.String("spanID", existingSpanID),
		)
	}

	next(rw, r)

	annotations = append(annotations, zipkinmodel.Annotation{Timestamp: time.Now(), Value: "ss"})

	traceIDHeader := r.Header.Get(b3.TraceID)
	spanIDHeader := r.Header.Get(b3.SpanID)
	if traceIDHeader != "" && spanIDHeader != "" {
		reqInfo, err := ContextRequestInfo(r)
		if err != nil {
			z.logger.Error("request-info-err", zap.Error(err))
			return
		}
		var appID, destIPandPort, appIndex, instanceId string
		if reqInfo.RouteEndpoint != nil {
			appID = reqInfo.RouteEndpoint.ApplicationId
			appIndex = reqInfo.RouteEndpoint.PrivateInstanceIndex
			destIPandPort = reqInfo.RouteEndpoint.CanonicalAddr()
			instanceId = reqInfo.RouteEndpoint.PrivateInstanceId
		}

		sc, _ := b3.ParseHeaders(traceIDHeader, spanIDHeader, r.Header.Get(b3.ParentSpanID), r.Header.Get(b3.Sampled), r.Header.Get(b3.Flags))
		// TODO: handle error

		cert, err := tls.X509KeyPair([]byte(z.collectorConfig.ClientCert), []byte(z.collectorConfig.ClientKey))
		if err != nil {
			z.logger.Error("zipking-collector-client-certificate-error",
				zap.Error(err),
			)
			return
		}

		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM([]byte(z.collectorConfig.CACert))

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      caCertPool,
		}

		zipkinClient := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
		reporter := zipkinhttp.NewReporter(z.collectorConfig.URL, zipkinhttp.Client(zipkinClient))
		reporter.Send(zipkinmodel.SpanModel{
			Name:        "serve-http",
			SpanContext: *sc,
			Kind:        zipkinmodel.Client,
			Timestamp:   startTime,
			Annotations: annotations,
			Tags: map[string]string{
				"app_id":           appID,
				"app_index":        appIndex,
				"instance_id":      instanceId,
				"addr":             destIPandPort,
				"status_code":      fmt.Sprintf("%d", proxyWriter.Status()),
				"x_cf_routererror": proxyWriter.Header().Get(router_http.CfRouterError),
			},
			RemoteEndpoint: &zipkinmodel.Endpoint{ServiceName: "gorouter"},
		})

		reporter.Close()
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
