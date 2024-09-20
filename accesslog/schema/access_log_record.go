package schema

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
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

// WriteDashOrIntValue writes an int or a "-" to the buffer if the int is
// equal to 0
func (b *recordBuffer) WriteDashOrIntValue(v int) {
	if v == 0 {
		_, _ = b.WriteString(`"-"`)
		b.writeSpace()
	} else {
		b.WriteIntValue(v)
	}
}

// WriteDashOrFloatValue writes a float if the value is >= 0 or a "-" if the
// value is negative to the buffer.
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
	BodyBytesSent          int
	RequestBytesReceived   int
	ExtraHeadersToLog      []string
	DisableXFFLogging      bool
	DisableSourceIPLogging bool
	RedactQueryParams      string
	RouterError            string
	LogAttemptsDetails     bool
	FailedAttempts         int
	RoundTripSuccessful    bool
	record                 []byte

	// See the handlers.RequestInfo struct for details on these timings.
	ReceivedAt                  time.Time
	AppRequestStartedAt         time.Time
	LastFailedAttemptFinishedAt time.Time
	DnsStartedAt                time.Time
	DnsFinishedAt               time.Time
	DialStartedAt               time.Time
	DialFinishedAt              time.Time
	TlsHandshakeStartedAt       time.Time
	TlsHandshakeFinishedAt      time.Time
	AppRequestFinishedAt        time.Time
	FinishedAt                  time.Time
}

func (r *AccessLogRecord) formatStartedAt() string {
	return r.ReceivedAt.Format("2006-01-02T15:04:05.000000000Z")
}

func (r *AccessLogRecord) roundtripTime() float64 {
	return r.FinishedAt.Sub(r.ReceivedAt).Seconds()
}

func (r *AccessLogRecord) gorouterTime() float64 {
	rt := r.roundtripTime()
	at := r.appTime()

	if rt >= 0 && at >= 0 {
		return rt - at
	} else {
		return -1
	}
}

func (r *AccessLogRecord) dialTime() float64 {
	if r.DialStartedAt.IsZero() || r.DialFinishedAt.IsZero() {
		return -1
	}
	return r.DialFinishedAt.Sub(r.DialStartedAt).Seconds()
}

func (r *AccessLogRecord) dnsTime() float64 {
	if r.DnsStartedAt.IsZero() || r.DnsFinishedAt.IsZero() {
		return -1
	}
	return r.DnsFinishedAt.Sub(r.DnsStartedAt).Seconds()
}

func (r *AccessLogRecord) tlsTime() float64 {
	if r.TlsHandshakeStartedAt.IsZero() || r.TlsHandshakeFinishedAt.IsZero() {
		return -1
	}
	return r.TlsHandshakeFinishedAt.Sub(r.TlsHandshakeStartedAt).Seconds()
}

func (r *AccessLogRecord) appTime() float64 {
	return r.AppRequestFinishedAt.Sub(r.AppRequestStartedAt).Seconds()
}

// failedAttemptsTime will be negative if there was no failed attempt.
func (r *AccessLogRecord) failedAttemptsTime() float64 {
	if r.LastFailedAttemptFinishedAt.IsZero() {
		return -1
	}
	return r.LastFailedAttemptFinishedAt.Sub(r.AppRequestStartedAt).Seconds()
}

// successfulAttemptTime returns -1 if there was an error, so no attempt was
// successful. If there was a successful attempt the returned time indicates
// how long it took.
func (r *AccessLogRecord) successfulAttemptTime() float64 {
	if !r.RoundTripSuccessful {
		return -1
	}

	// we only want the time of the successful attempt
	if !r.LastFailedAttemptFinishedAt.IsZero() {
		// exclude the time any failed attempts took
		return r.AppRequestFinishedAt.Sub(r.LastFailedAttemptFinishedAt).Seconds()
	} else {
		return r.AppRequestFinishedAt.Sub(r.AppRequestStartedAt).Seconds()
	}
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
	var appID, destIPandPort, appIndex, instanceId string

	if r.RouteEndpoint != nil {
		appID = r.RouteEndpoint.ApplicationId
		appIndex = r.RouteEndpoint.PrivateInstanceIndex
		destIPandPort = r.RouteEndpoint.CanonicalAddr()
		instanceId = r.RouteEndpoint.PrivateInstanceId
	}

	headers := r.Request.Header
	if r.HeadersOverride != nil {
		headers = r.HeadersOverride
	}

	b := new(recordBuffer)

	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	b.WriteString(r.Request.Host)
	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	b.WriteString(` - `)
	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
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

	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	b.WriteString(`x_forwarded_for:`)
	if r.DisableXFFLogging {
		b.WriteDashOrStringValue("-")
	} else {
		xForwardedFor := formatHeader(headers, "X-Forwarded-For", performTruncate)
		b.WriteDashOrStringValue(xForwardedFor)
	}

	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	b.WriteString(`x_forwarded_proto:`)
	xForwardedProto := formatHeader(headers, "X-Forwarded-Proto", performTruncate)
	b.WriteDashOrStringValue(xForwardedProto)

	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	b.WriteString(`vcap_request_id:`)
	b.WriteDashOrStringValue(headers.Get("X-Vcap-Request-Id"))

	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	b.WriteString(`response_time:`)
	b.WriteDashOrFloatValue(r.roundtripTime())

	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	b.WriteString(`gorouter_time:`)
	b.WriteDashOrFloatValue(r.gorouterTime())

	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	b.WriteString(`app_id:`)
	b.WriteDashOrStringValue(appID)

	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	b.WriteString(`app_index:`)
	b.WriteDashOrStringValue(appIndex)

	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	b.WriteString(`instance_id:`)
	b.WriteDashOrStringValue(instanceId)

	if r.LogAttemptsDetails {
		// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
		b.WriteString(`failed_attempts:`)
		b.WriteIntValue(r.FailedAttempts)

		// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
		b.WriteString(`failed_attempts_time:`)
		b.WriteDashOrFloatValue(r.failedAttemptsTime())

		// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
		b.WriteString(`dns_time:`)
		b.WriteDashOrFloatValue(r.dnsTime())

		// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
		b.WriteString(`dial_time:`)
		b.WriteDashOrFloatValue(r.dialTime())

		// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
		b.WriteString(`tls_time:`)
		b.WriteDashOrFloatValue(r.tlsTime())

		// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
		b.WriteString(`backend_time:`)
		b.WriteDashOrFloatValue(r.successfulAttemptTime())
	}

	b.AppendSpaces(false)
	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
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
			// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
			headerBuffer.WriteString("...EXTRA-REQUEST-HEADERS-TOO-LONG-TO-LOG--TRUNCATED")
			break
		}

		endOfRange := i == numExtraHeaders-1
		writeExtraHeader(headerBuffer, headerName, headerValue, endOfRange)
	}

	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	b.WriteByte(' ')
	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
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
	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
	buffer.WriteString(headerName)
	// #nosec  G104 - ignore errors from writing the access log as it will only cause more errors to log this error
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
