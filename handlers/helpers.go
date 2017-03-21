package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"code.cloudfoundry.org/gorouter/access_log/schema"
	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
)

func writeStatus(rw http.ResponseWriter, code int, message string, alr interface{}, logger logger.Logger) {
	body := fmt.Sprintf("%d %s: %s", code, http.StatusText(code), message)

	logger.Info("status", zap.String("body", body))
	if alr == nil {
		logger.Error("AccessLogRecord-not-set-on-context", zap.Error(errors.New("failed-to-access-log-record")))
	} else {
		accessLogRecord := alr.(*schema.AccessLogRecord)
		accessLogRecord.StatusCode = code
	}

	http.Error(rw, body, code)
	if code > 299 {
		rw.Header().Del("Connection")
	}
}

func hostWithoutPort(req *http.Request) string {
	host := req.Host

	// Remove :<port>
	pos := strings.Index(host, ":")
	if pos >= 0 {
		host = host[0:pos]
	}

	return host
}
