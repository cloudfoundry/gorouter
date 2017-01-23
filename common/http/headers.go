package http

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/uber-go/zap"

	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/logger"
)

const (
	VcapBackendHeader     = "X-Vcap-Backend"
	CfRouteEndpointHeader = "X-Cf-RouteEndpoint"
	VcapRouterHeader      = "X-Vcap-Router"
	VcapRequestIdHeader   = "X-Vcap-Request-Id"
	VcapTraceHeader       = "X-Vcap-Trace"
	CfInstanceIdHeader    = "X-CF-InstanceID"
	B3TraceIdHeader       = "X-B3-TraceId"
	B3SpanIdHeader        = "X-B3-SpanId"
	B3ParentSpanIdHeader  = "X-B3-ParentSpanId"
	CfAppInstance         = "X-CF-APP-INSTANCE"
)

func SetVcapRequestIdHeader(request *http.Request, logger logger.Logger) {
	guid, err := uuid.GenerateUUID()
	if err == nil {
		request.Header.Set(VcapRequestIdHeader, guid)
		if logger != nil {
			logger.Debug("vcap-request-id-header-set", zap.String("VcapRequestIdHeader", guid))
		}
	}
}

func SetTraceHeaders(responseWriter http.ResponseWriter, routerIp, addr string) {
	responseWriter.Header().Set(VcapRouterHeader, routerIp)
	responseWriter.Header().Set(VcapBackendHeader, addr)
	responseWriter.Header().Set(CfRouteEndpointHeader, addr)
}

func SetB3Headers(request *http.Request, logger logger.Logger) {
	existingTraceId := request.Header.Get(B3TraceIdHeader)
	existingSpanId := request.Header.Get(B3SpanIdHeader)
	if existingTraceId != "" && existingSpanId != "" {
		setB3SpanIdHeader(request, logger)
		setB3ParentSpanIdHeader(request, existingSpanId)
		if logger != nil {
			logger.Debug("b3-trace-id-header-exists", zap.String("B3TraceIdHeader", existingTraceId))
		}
		return
	}

	randBytes, err := secure.RandomBytes(8)
	if err != nil {
		logger.Info("failed-to-create-b3-trace-id", zap.Error(err))
		return
	}

	id := hex.EncodeToString(randBytes)
	request.Header.Set(B3TraceIdHeader, id)
	request.Header.Set(B3SpanIdHeader, request.Header.Get(B3TraceIdHeader))
}

func setB3ParentSpanIdHeader(request *http.Request, parentSpanID string) {
	request.Header.Set(B3ParentSpanIdHeader, parentSpanID)
}

func setB3SpanIdHeader(request *http.Request, logger logger.Logger) {
	randBytes, err := secure.RandomBytes(8)
	if err != nil {
		logger.Info("failed-to-create-b3-span-id", zap.Error(err))
		return
	}
	id := hex.EncodeToString(randBytes)
	request.Header.Set(B3SpanIdHeader, id)
}

func ValidateCfAppInstance(appInstanceHeader string) (string, string, error) {
	appDetails := strings.Split(appInstanceHeader, ":")
	if len(appDetails) != 2 {
		return "", "", fmt.Errorf("Incorrect %s header : %s", CfAppInstance, appInstanceHeader)
	}

	if appDetails[0] == "" || appDetails[1] == "" {
		return "", "", fmt.Errorf("Incorrect %s header : %s", CfAppInstance, appInstanceHeader)
	}

	return appDetails[0], appDetails[1], nil
}
