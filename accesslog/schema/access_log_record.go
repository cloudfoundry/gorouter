package schema

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/config"

	"code.cloudfoundry.org/gorouter/route"
)

//go:generate counterfeiter -o fakes/access_log_record.go . LogSender
type LogSender interface {
	SendAppLog(appID, message string, tags map[string]string)
}

// recordBuffer defines additional helper methods to write to the record buffer
type recordBuffer struct {
	bytes.Buffer
	spaces bool
}

// these limits are to make sure the log packet stays below 65k as required by
// UDP.
// * the SMALL_BYTES_LIMIT is for headers normally governed by a browser,
// including: referer, user-agent, x-forwarded-for, x-forwarded-proto.
// * the LARGE_BYTES_LIMIT is for "extra headers" (user set headers) and URIs
// including query params.
const SMALL_BYTES_LIMIT = 1_000
const LARGE_BYTES_LIMIT = 20_000

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
		_, _ = b.WriteString(strconv.FormatFloat(v, 'f', 6, 64))
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
	Request                *http.Request
	HeadersOverride        http.Header
	StatusCode             int
	RouteEndpoint          *route.Endpoint
	RouteServiceUrl        *url.URL
	RoundtripStartedAt     time.Time
	FirstByteAt            time.Time
	RoundtripFinishedAt    time.Time
	AppRequestStartedAt    time.Time
	AppRequestFinishedAt   time.Time
	BodyBytesSent          int
	RequestBytesReceived   int
	ExtraHeadersToLog      []string
	DisableXFFLogging      bool
	DisableSourceIPLogging bool
	RedactQueryParams      string
	RouterError            string
	record                 []byte
}

func (r *AccessLogRecord) formatStartedAt() string {
	return r.RoundtripStartedAt.Format("2006-01-02T15:04:05.000000000Z")
}

func (r *AccessLogRecord) roundtripTime() float64 {
	return float64(r.RoundtripFinishedAt.UnixNano()-r.RoundtripStartedAt.UnixNano()) / float64(time.Second)
}

func (r *AccessLogRecord) gorouterTime() float64 {
	rt := r.roundtripTime()
	at := r.appTime()
	if rt >= 0 && at >= 0 {
		return r.roundtripTime() - r.appTime()
	}
	return -1
}

func (r *AccessLogRecord) appTime() float64 {
	return float64(r.AppRequestFinishedAt.UnixNano()-r.AppRequestStartedAt.UnixNano()) / float64(time.Second)
}

func (r *AccessLogRecord) getRecord(performTruncate bool) []byte {
	recordLen := len(r.record)
	isEmpty := recordLen == 0
	recordTooBigForUDP := recordLen > 65_400

	// this optimizes for most cases where the record is small and does not
	// require truncation for UDP
	if isEmpty || (recordTooBigForUDP && performTruncate) {
		r.record = r.makeRecord(performTruncate)
	}

	return r.record
}

func (r *AccessLogRecord) makeRecord(performTruncate bool) []byte {
	var appID, destIPandPort, appIndex, instanceId, routeServiceUrl string

	if r.RouteEndpoint != nil {
		appID = r.RouteEndpoint.ApplicationId
		appIndex = r.RouteEndpoint.PrivateInstanceIndex
		destIPandPort = r.RouteEndpoint.CanonicalAddr()
		instanceId = r.RouteEndpoint.PrivateInstanceId
	}

	if r.RouteServiceUrl != nil {
		routeServiceUrl = truncateToSize(r.RouteServiceUrl.String(), "route-service-url", LARGE_BYTES_LIMIT)
	}

	headers := r.Request.Header
	if r.HeadersOverride != nil {
		headers = r.HeadersOverride
	}

	b := new(recordBuffer)

	b.WriteString(r.Request.Host)
	b.WriteString(` - `)
	b.WriteString(`[` + r.formatStartedAt() + `] `)

	b.AppendSpaces(true)

	uri := formatURI(*r, performTruncate)
	b.WriteStringValues(r.Request.Method, uri, r.Request.Proto)
	b.WriteDashOrIntValue(r.StatusCode)
	b.WriteIntValue(r.RequestBytesReceived)
	b.WriteIntValue(r.BodyBytesSent)

	referer := formatHeader(headers, "Referer", performTruncate)
	b.WriteDashOrStringValue(referer)

	userAgent := formatHeader(headers, "User-Agent", performTruncate)
	b.WriteDashOrStringValue(userAgent)

	if r.DisableSourceIPLogging {
		b.WriteDashOrStringValue("-")
	} else {
		b.WriteDashOrStringValue(r.Request.RemoteAddr)
	}

	b.WriteDashOrStringValue(destIPandPort)

	b.WriteString(`x_forwarded_for:`)
	if r.DisableXFFLogging {
		b.WriteDashOrStringValue("-")
	} else {
		xForwardedFor := formatHeader(headers, "X-Forwarded-For", performTruncate)
		b.WriteDashOrStringValue(xForwardedFor)
	}

	b.WriteString(`x_forwarded_proto:`)
	xForwardedProto := formatHeader(headers, "X-Forwarded-Proto", performTruncate)
	b.WriteDashOrStringValue(xForwardedProto)

	b.WriteString(`vcap_request_id:`)
	b.WriteDashOrStringValue(headers.Get("X-Vcap-Request-Id"))

	b.WriteString(`response_time:`)
	b.WriteDashOrFloatValue(r.roundtripTime())

	b.WriteString(`gorouter_time:`)
	b.WriteDashOrFloatValue(r.gorouterTime())

	b.WriteString(`app_id:`)
	b.WriteDashOrStringValue(appID)

	b.WriteString(`app_index:`)
	b.WriteDashOrStringValue(appIndex)

	b.WriteString(`instance_id:`)
	b.WriteDashOrStringValue(instanceId)

	b.WriteString(`route_service_url:`)
	b.WriteDashOrStringValue(routeServiceUrl)

	b.AppendSpaces(false)
	b.WriteString(`x_cf_routererror:`)
	b.WriteDashOrStringValue(r.RouterError)

	r.addExtraHeaders(b, performTruncate)

	return b.Bytes()
}

func formatURI(r AccessLogRecord, performTruncate bool) string {
	uri := redactURI(r)
	if performTruncate {
		return truncateToSize(uri, "request-uri", LARGE_BYTES_LIMIT)
	}
	return uri
}

// Redact query parameters on GET requests that have a query part
func redactURI(r AccessLogRecord) string {
	if r.Request.Method == http.MethodGet {
		if r.Request.URL.RawQuery != "" {
			switch r.RedactQueryParams {
			case config.REDACT_QUERY_PARMS_ALL:
				r.Request.URL.RawQuery = ""
			case config.REDACT_QUERY_PARMS_HASH:
				hash := sha1.New()
				hash.Write([]byte(r.Request.URL.RawQuery))
				hashString := hex.EncodeToString(hash.Sum(nil))
				r.Request.URL.RawQuery = fmt.Sprintf("hash=%s", hashString)
			}
		}
	}

	return r.Request.URL.RequestURI()
}

func truncateToSize(value, name string, limit int) string {
	for len(value) > limit {
		value = value[0:len(value)/2] + fmt.Sprintf("...%s-TOO-LONG-TO-LOG--TRUNCATED", strings.ToUpper(name))
	}
	return value
}

func formatHeader(headers http.Header, name string, performTruncate bool) string {
	value := headers.Get(name)
	if performTruncate {
		return truncateToSize(value, name, SMALL_BYTES_LIMIT)
	}
	return value
}

// WriteTo allows the AccessLogRecord to implement the io.WriterTo interface
func (r *AccessLogRecord) WriteTo(w io.Writer) (int64, error) {
	bytesWritten, err := w.Write(r.getRecord(false))
	if err != nil {
		return int64(bytesWritten), err
	}
	newline, err := w.Write([]byte("\n"))
	return int64(bytesWritten + newline), err
}

func (r *AccessLogRecord) SendLog(ls LogSender) {
	ls.SendAppLog(r.ApplicationID(), r.LogMessage(), r.tags())
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

	return string(r.getRecord(true))
}

func (r *AccessLogRecord) tags() map[string]string {
	if r.RouteEndpoint == nil {
		return nil
	}

	return r.RouteEndpoint.Tags
}

func (r *AccessLogRecord) addExtraHeaders(b *recordBuffer, performTruncate bool) {
	if r.ExtraHeadersToLog == nil {
		return
	}
	numExtraHeaders := len(r.ExtraHeadersToLog)
	if numExtraHeaders == 0 {
		return
	}

	headerBuffer := new(recordBuffer)
	headerBuffer.AppendSpaces(true)

	for i, header := range r.ExtraHeadersToLog {
		headerName, headerValue, headerLength := r.processExtraHeader(header)

		anticipatedLength := headerBuffer.Buffer.Len() + headerLength

		// ensure what we're about to append is under our limit for headers
		if extraHeaderNeedsTruncate(anticipatedLength, performTruncate) {
			headerBuffer.WriteString("...EXTRA-REQUEST-HEADERS-TOO-LONG-TO-LOG--TRUNCATED")
			break
		}

		endOfRange := i == numExtraHeaders-1
		writeExtraHeader(headerBuffer, headerName, headerValue, endOfRange)
	}

	b.WriteByte(' ')
	b.Write(headerBuffer.Bytes())
}

func extraHeaderNeedsTruncate(length int, performTruncate bool) bool {
	return performTruncate && length >= LARGE_BYTES_LIMIT
}

func (r *AccessLogRecord) processExtraHeader(header string) (headerName string, headerValue string, headerLength int) {
	// X-Something-Cool -> x_something_cool
	headerName = strings.Replace(strings.ToLower(header), "-", "_", -1)
	headerValue = r.Request.Header.Get(header)

	headerLength = getExtraHeaderLengthInBytes(headerName, headerValue)

	return
}

func writeExtraHeader(buffer *recordBuffer, headerName string, headerValue string, endOfRange bool) {
	buffer.WriteString(headerName)
	buffer.WriteByte(':')
	if endOfRange {
		buffer.AppendSpaces(false)
	}
	buffer.WriteDashOrStringValue(headerValue)
}

func getExtraHeaderLengthInBytes(headerKey, headerValue string) int {

	// final record will surround values with quotes, space-separate headers,
	// and colon-delimit key from value
	headerLength := len(headerKey) + len(headerValue) + 4

	if headerValue == "" {
		// if no value, we specify `"-"`, so compensate length here
		headerLength += 3
	}
	return headerLength
}
