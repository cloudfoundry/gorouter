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
