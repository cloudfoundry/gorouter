package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
)

func writeStatus(rw http.ResponseWriter, code int, message string, logger logger.Logger) {
	body := fmt.Sprintf("%d %s: %s", code, http.StatusText(code), message)

	logger.Info("status", zap.String("body", body))

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

func IsTcpUpgrade(request *http.Request) bool {
	return upgradeHeader(request) == "tcp"
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
