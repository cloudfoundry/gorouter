package schema

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/route"
)

// recordBuffer defines additional helper methods to write to the record buffer
type recordBuffer struct {
	bytes.Buffer
	spaces bool
}

// AppendSpaces allows the recordBuffer to automatically append spaces
// after each write operation defined here if the arg is true
func (b *recordBuffer) AppendSpaces(arg bool) {
	b.spaces = arg
}

// writeSpace writes a space to the buffer if ToggleAppendSpaces is set
func (b *recordBuffer) writeSpace() {
	if b.spaces {
		_ = b.WriteByte(' ')
	}
}

// WriteIntValue writes an int to the buffer
func (b *recordBuffer) WriteIntValue(v int) {
	_, _ = b.WriteString(strconv.Itoa(v))
	b.writeSpace()
}

// WriteDashOrStringValue writes an int or a "-" to the buffer if the int is
// equal to 0
func (b *recordBuffer) WriteDashOrIntValue(v int) {
	if v == 0 {
		_, _ = b.WriteString(`"-"`)
		b.writeSpace()
	} else {
		b.WriteIntValue(v)
	}
}

// WriteDashOrStringValue writes a float or a "-" to the buffer if the float is
// 0 or lower
func (b *recordBuffer) WriteDashOrFloatValue(v float64) {
	if v >= 0 {
		_, _ = b.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
	} else {
		_, _ = b.WriteString(`"-"`)
	}
	b.writeSpace()
}

// WriteStringValues always writes quoted strings to the buffer
func (b *recordBuffer) WriteStringValues(s ...string) {
	var t []byte
	t = strconv.AppendQuote(t, strings.Join(s, ` `))
	_, _ = b.Write(t)
	b.writeSpace()
}

// WriteDashOrStringValue writes quoted strings or a "-" if the string is empty
func (b *recordBuffer) WriteDashOrStringValue(s string) {
	if s == "" {
		_, _ = b.WriteString(`"-"`)
		b.writeSpace()
	} else {
		b.WriteStringValues(s)
	}
}

// AccessLogRecord represents a single access log line
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
	record               []byte
}

func (r *AccessLogRecord) formatStartedAt() string {
	return r.StartedAt.Format("2006-01-02T15:04:05.000-0700")
}

func (r *AccessLogRecord) responseTime() float64 {
	return float64(r.FinishedAt.UnixNano()-r.StartedAt.UnixNano()) / float64(time.Second)
}

// getRecord memoizes makeRecord()
func (r *AccessLogRecord) getRecord() []byte {
	if len(r.record) == 0 {
		r.record = r.makeRecord()
	}

	return r.record
}

func (r *AccessLogRecord) makeRecord() []byte {
	var appID, destIPandPort, appIndex string

	if r.RouteEndpoint != nil {
		appID = r.RouteEndpoint.ApplicationId
		appIndex = r.RouteEndpoint.PrivateInstanceIndex
		destIPandPort = r.RouteEndpoint.CanonicalAddr()
	}

	b := new(recordBuffer)

	b.WriteString(r.Request.Host)
	b.WriteString(` - `)
	b.WriteString(`[` + r.formatStartedAt() + `] `)

	b.AppendSpaces(true)
	b.WriteStringValues(r.Request.Method, r.Request.URL.RequestURI(), r.Request.Proto)
	b.WriteDashOrIntValue(r.StatusCode)
	b.WriteIntValue(r.RequestBytesReceived)
	b.WriteIntValue(r.BodyBytesSent)
	b.WriteDashOrStringValue(r.Request.Header.Get("Referer"))
	b.WriteDashOrStringValue(r.Request.Header.Get("User-Agent"))
	b.WriteDashOrStringValue(r.Request.RemoteAddr)
	b.WriteDashOrStringValue(destIPandPort)

	b.WriteString(`x_forwarded_for:`)
	b.WriteDashOrStringValue(r.Request.Header.Get("X-Forwarded-For"))

	b.WriteString(`x_forwarded_proto:`)
	b.WriteDashOrStringValue(r.Request.Header.Get("X-Forwarded-Proto"))

	b.WriteString(`vcap_request_id:`)
	b.WriteDashOrStringValue(r.Request.Header.Get("X-Vcap-Request-Id"))

	b.WriteString(`response_time:`)
	b.WriteDashOrFloatValue(r.responseTime())

	b.WriteString(`app_id:`)
	b.WriteDashOrStringValue(appID)

	b.AppendSpaces(false)
	b.WriteString(`app_index:`)
	b.WriteDashOrStringValue(appIndex)

	r.addExtraHeaders(b)

	b.WriteByte('\n')

	return b.Bytes()
}

// WriteTo allows the AccessLogRecord to implement the io.WriterTo interface
func (r *AccessLogRecord) WriteTo(w io.Writer) (int64, error) {
	bytesWritten, err := w.Write(r.getRecord())
	return int64(bytesWritten), err
}

// ApplicationID returns the application ID that corresponds with the access log
func (r *AccessLogRecord) ApplicationID() string {
	if r.RouteEndpoint == nil {
		return ""
	}

	return r.RouteEndpoint.ApplicationId
}

// LogMessage returns a string representation of the access log line
func (r *AccessLogRecord) LogMessage() string {
	if r.ApplicationID() == "" {
		return ""
	}

	return string(r.getRecord())
}

func (r *AccessLogRecord) addExtraHeaders(b *recordBuffer) {
	if r.ExtraHeadersToLog == nil {
		return
	}
	numExtraHeaders := len(r.ExtraHeadersToLog)
	if numExtraHeaders == 0 {
		return
	}

	b.WriteByte(' ')
	b.AppendSpaces(true)
	for i, header := range r.ExtraHeadersToLog {
		// X-Something-Cool -> x_something_cool
		headerName := strings.Replace(strings.ToLower(header), "-", "_", -1)
		b.WriteString(headerName)
		b.WriteByte(':')
		if i == numExtraHeaders-1 {
			b.AppendSpaces(false)
		}
		b.WriteDashOrStringValue(r.Request.Header.Get(header))
	}
}
