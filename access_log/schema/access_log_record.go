package schema

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudfoundry/gorouter/route"
)

type AccessLogRecord struct {
	Request              *http.Request
	StatusCode           int
	RouteEndpoint        *route.Endpoint
	StartedAt            time.Time
	FirstByteAt          time.Time
	FinishedAt           time.Time
	BodyBytesSent        int
	RequestBytesReceived int
	ExtraHeadersToLog    []string
	record               string
}

func (r *AccessLogRecord) FormatStartedAt() string {
	return r.StartedAt.Format("02/01/2006:15:04:05.000 -0700")
}

func (r *AccessLogRecord) FormatRequestHeader(k string) (v string) {
	v = r.Request.Header.Get(k)
	if v == "" {
		v = "-"
	}
	return
}

func (r *AccessLogRecord) ResponseTime() float64 {
	return float64(r.FinishedAt.UnixNano()-r.StartedAt.UnixNano()) / float64(time.Second)
}

// memoize makeRecord()
func (r *AccessLogRecord) getRecord() string {
	if len(r.record) == 0 {
		r.record = r.makeRecord()
	}

	return r.record
}

func (r *AccessLogRecord) makeRecord() string {
	statusCode, responseTime, appId, extraHeaders := "-", "-", "-", ""

	if r.StatusCode != 0 {
		statusCode = strconv.Itoa(r.StatusCode)
	}

	if r.ResponseTime() >= 0 {
		responseTime = strconv.FormatFloat(r.ResponseTime(), 'f', -1, 64)
	}

	if r.RouteEndpoint != nil {
		appId = r.RouteEndpoint.ApplicationId
	}

	if r.ExtraHeadersToLog != nil && len(r.ExtraHeadersToLog) > 0 {
		extraHeaders = r.ExtraHeaders()
	}

	return fmt.Sprintf(`%s - [%s] "%s %s %s" %s %d %d "%s" "%s" %s x_forwarded_for:"%s" x_forwarded_proto:"%s" vcap_request_id:%s response_time:%s app_id:%s%s`+"\n",
		r.Request.Host,
		r.FormatStartedAt(),
		r.Request.Method,
		r.Request.URL.RequestURI(),
		r.Request.Proto,
		statusCode,
		r.RequestBytesReceived,
		r.BodyBytesSent,
		r.FormatRequestHeader("Referer"),
		r.FormatRequestHeader("User-Agent"),
		r.Request.RemoteAddr,
		r.FormatRequestHeader("X-Forwarded-For"),
		r.FormatRequestHeader("X-Forwarded-Proto"),
		r.FormatRequestHeader("X-Vcap-Request-Id"),
		responseTime,
		appId,
		extraHeaders)
}

func (r *AccessLogRecord) WriteTo(w io.Writer) (int64, error) {
	recordBuffer := bytes.NewBufferString(r.getRecord())
	return recordBuffer.WriteTo(w)
}

func (r *AccessLogRecord) ApplicationId() string {
	if r.RouteEndpoint == nil || r.RouteEndpoint.ApplicationId == "" {
		return ""
	}

	return r.RouteEndpoint.ApplicationId
}

func (r *AccessLogRecord) LogMessage() string {
	if r.ApplicationId() == "" {
		return ""
	}

	return r.getRecord()
}

func (r *AccessLogRecord) ExtraHeaders() string {
	extraHeaders := make([]string, 0, len(r.ExtraHeadersToLog)*4)

	for _, header := range r.ExtraHeadersToLog {
		// X-Something-Cool -> x_something_cool
		headerName := strings.Replace(strings.ToLower(header), "-", "_", -1)
		headerValue := strconv.Quote(r.FormatRequestHeader(header))
		extraHeaders = append(extraHeaders, " ", headerName, ":", headerValue)
	}

	return strings.Join(extraHeaders, "")
}
