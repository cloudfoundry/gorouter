package handlers

import (
	"bufio"
	"errors"
	"net"
	"net/http"

	"fmt"

	"code.cloudfoundry.org/gorouter/access_log/schema"
	"code.cloudfoundry.org/gorouter/logger"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

type protocolCheck struct {
	logger logger.Logger
}

// NewProtocolCheck creates a handler responsible for checking the protocol of
// the request
func NewProtocolCheck(logger logger.Logger) negroni.Handler {
	return &protocolCheck{
		logger: logger,
	}
}

func (p *protocolCheck) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if !isProtocolSupported(r) {
		var accessLogRecord *schema.AccessLogRecord
		alr := r.Context().Value("AccessLogRecord")
		if alr == nil {
			p.logger.Error("AccessLogRecord-not-set-on-context", zap.Error(errors.New("failed-to-access-log-record")))
		} else {
			accessLogRecord = alr.(*schema.AccessLogRecord)
		}
		// must be hijacked, otherwise no response is sent back
		conn, buf, err := p.hijack(rw)
		if err != nil {
			code := http.StatusBadRequest
			body := fmt.Sprintf("%d %s: %s", code, http.StatusText(code), "Unsupported protocol")

			p.logger.Info("status", zap.String("body", body))
			if accessLogRecord != nil {
				accessLogRecord.StatusCode = code
			}

			http.Error(rw, body, code)
			rw.Header().Del("Connection")
			return
		}

		if accessLogRecord != nil {
			accessLogRecord.StatusCode = http.StatusBadRequest
		}
		fmt.Fprintf(buf, "HTTP/1.0 400 Bad Request\r\n\r\n")
		buf.Flush()
		conn.Close()
		return
	}

	next(rw, r)
}

func (p *protocolCheck) hijack(rw http.ResponseWriter) (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer cannot hijack")
	}
	return hijacker.Hijack()
}

func isProtocolSupported(request *http.Request) bool {
	return request.ProtoMajor == 1 && (request.ProtoMinor == 0 || request.ProtoMinor == 1)
}
