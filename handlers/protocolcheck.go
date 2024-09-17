package handlers

import (
	"bufio"
	"errors"
	"net"
	"net/http"

	"fmt"

	"code.cloudfoundry.org/gorouter/errorwriter"
	"code.cloudfoundry.org/gorouter/logger"
	"github.com/urfave/negroni/v3"
)

type protocolCheck struct {
	logger      logger.Logger
	errorWriter errorwriter.ErrorWriter
	enableHTTP2 bool
}

// NewProtocolCheck creates a handler responsible for checking the protocol of
// the request
func NewProtocolCheck(logger logger.Logger, errorWriter errorwriter.ErrorWriter, enableHTTP2 bool) negroni.Handler {
	return &protocolCheck{
		logger:      logger,
		errorWriter: errorWriter,
		enableHTTP2: enableHTTP2,
	}
}

func (p *protocolCheck) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	logger := LoggerWithTraceInfo(p.logger, r)
	if !p.isProtocolSupported(r) {
		// must be hijacked, otherwise no response is sent back
		conn, buf, err := p.hijack(rw)
		if err != nil {
			p.errorWriter.WriteError(
				rw,
				http.StatusBadRequest,
				"Unsupported protocol",
				logger,
			)
			return
		}

		fmt.Fprintf(buf, "HTTP/1.0 400 Bad Request\r\n\r\n")
		// #nosec G104 - ignore errors when writing HTTP responses so we don't spam our logs during a DoS
		buf.Flush()
		// #nosec G104 - ignore errors when writing HTTP responses so we don't spam our logs during a DoS
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

func (p *protocolCheck) isProtocolSupported(request *http.Request) bool {
	return (p.enableHTTP2 && request.ProtoMajor == 2) || (request.ProtoMajor == 1 && (request.ProtoMinor == 0 || request.ProtoMinor == 1))
}
