package access_log

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
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
}

func (r *AccessLogRecord) FormatStartedAt() string {
	return r.StartedAt.Format("02/01/2006:15:04:05 -0700")
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

func (r *AccessLogRecord) makeRecord() *bytes.Buffer {
	b := &bytes.Buffer{}
	fmt.Fprintf(b, `%s - `, r.Request.Host)
	fmt.Fprintf(b, `[%s] `, r.FormatStartedAt())
	fmt.Fprintf(b, `"%s %s %s" `, r.Request.Method, r.Request.URL.RequestURI(), r.Request.Proto)

	if r.StatusCode == 0 {
		fmt.Fprintf(b, "MissingResponseStatusCode ")
	} else {
		fmt.Fprintf(b, `%d `, r.StatusCode)
	}

	fmt.Fprintf(b, `%d `, r.RequestBytesReceived)
	fmt.Fprintf(b, `%d `, r.BodyBytesSent)
	fmt.Fprintf(b, `"%s" `, r.FormatRequestHeader("Referer"))
	fmt.Fprintf(b, `"%s" `, r.FormatRequestHeader("User-Agent"))
	fmt.Fprintf(b, `%s `, r.Request.RemoteAddr)
	fmt.Fprintf(b, `x_forwarded_for:"%s" `, r.FormatRequestHeader("X-Forwarded-For"))
	fmt.Fprintf(b, `vcap_request_id:%s `, r.FormatRequestHeader("X-Vcap-Request-Id"))

	if r.ResponseTime() < 0 {
		fmt.Fprintf(b, "response_time:MissingFinishedAt ")
	} else {
		fmt.Fprintf(b, `response_time:%.9f `, r.ResponseTime())
	}

	if r.RouteEndpoint == nil {
		fmt.Fprintf(b, "app_id:MissingRouteEndpointApplicationId")
	} else {
		fmt.Fprintf(b, `app_id:%s`, r.RouteEndpoint.ApplicationId)
	}

	if r.ExtraHeadersToLog != nil && len(r.ExtraHeadersToLog) > 0 {
		fmt.Fprintf(b, ` %s`, r.ExtraHeaders())
	}

	fmt.Fprint(b, "\n")
	return b
}

func (r *AccessLogRecord) WriteTo(w io.Writer) (int64, error) {
	recordBuffer := r.makeRecord()
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

	recordBuffer := r.makeRecord()
	return recordBuffer.String()
}

func (r *AccessLogRecord) ExtraHeaders() string {
	extraHeaders := []string{}
	for _, header := range r.ExtraHeadersToLog {
		// X-Something-Cool -> x_something_cool
		formatted_header_name := strings.Replace(strings.ToLower(header), "-", "_", -1)
		escaped_header_value := strings.Replace(r.FormatRequestHeader(header), "\"", "\\\"", -1)
		headerString := fmt.Sprintf("%s:\"%s\"", formatted_header_name, escaped_header_value)
		extraHeaders = append(extraHeaders, headerString)
	}
	return strings.Join(extraHeaders, " ")
}
