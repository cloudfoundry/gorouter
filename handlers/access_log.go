package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sync/atomic"
	"time"

	"github.com/urfave/negroni/v3"

	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/accesslog/schema"
	router_http "code.cloudfoundry.org/gorouter/common/http"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/proxy/utils"
)

type accessLog struct {
	accessLogger       accesslog.AccessLogger
	extraHeadersToLog  []string
	logAttemptsDetails bool
	extraFields        []string
	logger             *slog.Logger
}

// NewAccessLog creates a new handler that handles logging requests to the
// access log
func NewAccessLog(
	accessLogger accesslog.AccessLogger,
	extraHeadersToLog []string,
	logAttemptsDetails bool,
	extraFields []string,
	logger *slog.Logger,
) negroni.Handler {
	return &accessLog{
		accessLogger:       accessLogger,
		extraHeadersToLog:  extraHeadersToLog,
		logAttemptsDetails: logAttemptsDetails,
		extraFields:        deduplicate(extraFields),
		logger:             logger,
	}
}

func (a *accessLog) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	proxyWriter := rw.(utils.ProxyResponseWriter)

	alr := &schema.AccessLogRecord{
		Request:            r,
		ExtraHeadersToLog:  a.extraHeadersToLog,
		LogAttemptsDetails: a.logAttemptsDetails,
		ExtraFields:        a.extraFields,
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

	alr.LocalAddress = reqInfo.LocalAddress

	a.accessLogger.Log(*alr)
}

type countingReadCloser struct {
	delegate io.ReadCloser
	count    uint64
}

func (crc *countingReadCloser) Read(b []byte) (int, error) {
	n, err := crc.delegate.Read(b)
	// #nosec G115 - we should never have a negative number of bytes read, so no overflow issues here
	atomic.AddUint64(&crc.count, uint64(n))
	return n, err
}

func (crc *countingReadCloser) GetCount() int {
	// #nosec G115 - we would only have overflow issues here if an http response was more than 9,223,372,036,854,775,807 bytes.
	return int(atomic.LoadUint64(&crc.count))
}

func (crc *countingReadCloser) Close() error {
	return crc.delegate.Close()
}

func deduplicate[S ~[]E, E comparable](s S) S {
	// costs some memory and requires an allocation but reduces complexity from O(n^2)
	// to O(n) where n = len(s)
	m := make(map[E]struct{}, len(s))
	return slices.DeleteFunc(s, func(s E) bool {
		_, ok := m[s]
		if ok {
			return true
		}

		m[s] = struct{}{}

		return false
	})
}
