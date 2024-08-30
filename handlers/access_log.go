package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/accesslog/schema"
	router_http "code.cloudfoundry.org/gorouter/common/http"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"github.com/urfave/negroni/v3"
)

type accessLog struct {
	accessLogger       accesslog.AccessLogger
	extraHeadersToLog  []string
	logAttemptsDetails bool
	logger             *slog.Logger
}

// NewAccessLog creates a new handler that handles logging requests to the
// access log
func NewAccessLog(
	accessLogger accesslog.AccessLogger,
	extraHeadersToLog []string,
	logAttemptsDetails bool,
	logger *slog.Logger,
) negroni.Handler {
	return &accessLog{
		accessLogger:       accessLogger,
		extraHeadersToLog:  extraHeadersToLog,
		logAttemptsDetails: logAttemptsDetails,
		logger:             logger,
	}
}

func (a *accessLog) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	proxyWriter := rw.(utils.ProxyResponseWriter)

	alr := &schema.AccessLogRecord{
		Request:            r,
		ExtraHeadersToLog:  a.extraHeadersToLog,
		LogAttemptsDetails: a.logAttemptsDetails,
	}

	requestBodyCounter := &countingReadCloser{delegate: r.Body}
	r.Body = requestBodyCounter

	next(rw, r)

	reqInfo, err := ContextRequestInfo(r)
	if err != nil {
		log.Panic(a.logger, "request-info-err", log.ErrAttr(err))
		return
	}

	reqInfo.FinishedAt = time.Now()

	alr.HeadersOverride = reqInfo.BackendReqHeaders
	alr.RouteEndpoint = reqInfo.RouteEndpoint
	alr.RequestBytesReceived = requestBodyCounter.GetCount()
	alr.BodyBytesSent = proxyWriter.Size()
	alr.StatusCode = proxyWriter.Status()
	alr.RouterError = proxyWriter.Header().Get(router_http.CfRouterError)
	alr.FailedAttempts = reqInfo.FailedAttempts
	alr.RoundTripSuccessful = reqInfo.RoundTripSuccessful

	alr.ReceivedAt = reqInfo.ReceivedAt
	alr.AppRequestStartedAt = reqInfo.AppRequestStartedAt
	alr.LastFailedAttemptFinishedAt = reqInfo.LastFailedAttemptFinishedAt
	alr.DnsStartedAt = reqInfo.DnsStartedAt
	alr.DnsFinishedAt = reqInfo.DnsFinishedAt
	alr.DialStartedAt = reqInfo.DialStartedAt
	alr.DialFinishedAt = reqInfo.DialFinishedAt
	alr.TlsHandshakeStartedAt = reqInfo.TlsHandshakeStartedAt
	alr.TlsHandshakeFinishedAt = reqInfo.TlsHandshakeFinishedAt
	alr.AppRequestFinishedAt = reqInfo.AppRequestFinishedAt
	alr.FinishedAt = reqInfo.FinishedAt

	a.accessLogger.Log(*alr)
}

type countingReadCloser struct {
	delegate io.ReadCloser
	count    uint32
}

func (crc *countingReadCloser) Read(b []byte) (int, error) {
	n, err := crc.delegate.Read(b)
	atomic.AddUint32(&crc.count, uint32(n))
	return n, err
}

func (crc *countingReadCloser) GetCount() int {
	return int(atomic.LoadUint32(&crc.count))
}

func (crc *countingReadCloser) Close() error {
	return crc.delegate.Close()
}
