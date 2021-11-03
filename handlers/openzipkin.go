package handlers

import (
	"net/http"

	"code.cloudfoundry.org/gorouter/logger"
	openZipkin "github.com/openzipkin/zipkin-go"
	openZipkinhttp "github.com/openzipkin/zipkin-go/middleware/http"
	openZipkinReporter "github.com/openzipkin/zipkin-go/reporter"
)

func NewOpenZipkin(zipkinEnabled bool, logger logger.Logger) func(http.Handler) http.Handler {
	openZipkinReport := createOpenZipkinReporter(zipkinEnabled)
	openZipkinTracer := createOpenZipkinTracer(zipkinEnabled, openZipkinReport)

	openZipkinHandler := openZipkinhttp.NewServerMiddleware(openZipkinTracer)

	return openZipkinHandler.handler
}

func createOpenZipkinReporter(zipkinEnabled bool) openZipkinReporter.Reporter {

	openZipkinReport := openZipkinReporter.NewNoopReporter()

	return openZipkinReport
}

func createOpenZipkinTracer(zipkinEnabled bool, openZipkinReporter openZipkinReporter.Reporter) *openZipkin.Tracer {
	option := openZipkin.WithNoopTracer(!zipkinEnabled)
	tracer, _ := openZipkin.NewTracer(openZipkinReporter, option)

	return tracer
}
