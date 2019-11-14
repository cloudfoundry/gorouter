package handlers

import (
	"fmt"
	"net/http"
	"strings"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
)

const (
	cacheMaxAgeSeconds = 2
)

func AddRouterErrorHeader(rw http.ResponseWriter, val string) {
	rw.Header().Set(router_http.CfRouterError, val)
}

func addInvalidResponseCacheControlHeader(rw http.ResponseWriter) {
	rw.Header().Set(
		"Cache-Control",
		fmt.Sprintf("public,max-age=%d", cacheMaxAgeSeconds),
	)
}

func writeStatus(rw http.ResponseWriter, code int, message string, logger logger.Logger) {
	body := fmt.Sprintf("%d %s: %s", code, http.StatusText(code), message)

	if code != 404 {
		logger.Info("status", zap.String("body", body))
	}

	http.Error(rw, body, code)
	if code > 299 {
		rw.Header().Del("Connection")
	}
}

func hostWithoutPort(reqHost string) string {
	host := reqHost

	// Remove :<port>
	pos := strings.Index(host, ":")
	if pos >= 0 {
		host = host[0:pos]
	}

	return host
}

func IsWebSocketUpgrade(request *http.Request) bool {
	// websocket should be case insensitive per RFC6455 4.2.1
	return strings.ToLower(upgradeHeader(request)) == "websocket"
}

func upgradeHeader(request *http.Request) string {
	// handle multiple Connection field-values, either in a comma-separated string or multiple field-headers
	for _, v := range request.Header[http.CanonicalHeaderKey("Connection")] {
		// upgrade should be case insensitive per RFC6455 4.2.1
		if strings.Contains(strings.ToLower(v), "upgrade") {
			return request.Header.Get("Upgrade")
		}
	}

	return ""
}
