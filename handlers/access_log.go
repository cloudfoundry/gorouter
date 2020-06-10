package handlers

import (
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/accesslog/schema"
	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/proxy/utils"

	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

type accessLog struct {
	accessLogger      accesslog.AccessLogger
	extraHeadersToLog []string
	logger            logger.Logger
}

// NewAccessLog creates a new handler that handles logging requests to the
// access log
func NewAccessLog(
	accessLogger accesslog.AccessLogger,
	extraHeadersToLog []string,
	logger logger.Logger,
) negroni.Handler {
	return &accessLog{
		accessLogger:      accessLogger,
		extraHeadersToLog: extraHeadersToLog,
		logger:            logger,
	}
}

func (a *accessLog) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	proxyWriter := rw.(utils.ProxyResponseWriter)

	alr := &schema.AccessLogRecord{
		Request:           r,
		ExtraHeadersToLog: a.extraHeadersToLog,
	}

	requestBodyCounter := &countingReadCloser{delegate: r.Body}
	r.Body = requestBodyCounter

	next(rw, r)

	reqInfo, err := ContextRequestInfo(r)
	if err != nil {
		a.logger.Fatal("request-info-err", zap.Error(err))
		return
	}

	alr.RoundtripStartedAt = reqInfo.StartedAt
	alr.RoundtripFinishedAt = time.Now()
	alr.AppRequestStartedAt = reqInfo.AppRequestStartedAt
	alr.AppRequestFinishedAt = reqInfo.AppRequestFinishedAt
	alr.HeadersOverride = reqInfo.BackendReqHeaders
	alr.RouteEndpoint = reqInfo.RouteEndpoint
	alr.RequestBytesReceived = requestBodyCounter.GetCount()
	alr.BodyBytesSent = proxyWriter.Size()
	alr.StatusCode = proxyWriter.Status()
	alr.RouterError = proxyWriter.Header().Get(router_http.CfRouterError)

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
