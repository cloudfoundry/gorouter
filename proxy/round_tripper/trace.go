package round_tripper

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"
)

// requestTracer holds trace data of a single request.
type requestTracer struct {
	gotConn      atomic.Bool
	connInfo     atomic.Pointer[httptrace.GotConnInfo]
	wroteHeaders atomic.Bool
	tDNSStart    atomic.Int64
	tDNSDone     atomic.Int64
	tDialStart   atomic.Int64
	tDialDone    atomic.Int64
	tTLSStart    atomic.Int64
	tTLSDone     atomic.Int64
}

// Reset the trace data. Helpful when performing the same request again.
func (t *requestTracer) Reset() {
	t.gotConn.Store(false)
	t.connInfo.Store(nil)
	t.wroteHeaders.Store(false)
	t.tDNSStart.Store(0)
	t.tDNSDone.Store(0)
	t.tDialStart.Store(0)
	t.tDialDone.Store(0)
	t.tTLSStart.Store(0)
	t.tTLSDone.Store(0)
}

// GotConn returns true if a connection (TCP + TLS) to the backend was established on the traced request.
func (t *requestTracer) GotConn() bool {
	return t.gotConn.Load()
}

// WroteHeaders returns true if HTTP headers were written on the traced request.
func (t *requestTracer) WroteHeaders() bool {
	return t.wroteHeaders.Load()
}

// ConnReused returns true if the traced request used an idle connection.
// it returns false if no idle connection was used or if the information was unavailable.
func (t *requestTracer) ConnReused() bool {
	info := t.connInfo.Load()
	if info != nil {
		return info.Reused
	}
	return false
}

// timeDelta returns the duration from t1 to t2.
// t1 and t2 are expected to be derived from time.UnixNano()
// it returns -1 if the duration isn't positive.
func timeDelta(t1, t2 int64) float64 {
	d := float64(t2-t1) / float64(time.Second)
	if d < 0 {
		d = -1
	}
	return d
}

// DnsTime returns the time taken for the DNS lookup of the traced request.
// in error cases the time will be set to -1.
func (t *requestTracer) DnsTime() float64 {
	return timeDelta(t.tDNSStart.Load(), t.tDNSDone.Load())
}

// DialTime returns the time taken for the TCP handshake of the traced request.
// in error cases the time will be set to -1.
func (t *requestTracer) DialTime() float64 {
	return timeDelta(t.tDialStart.Load(), t.tDialDone.Load())
}

// TlsTime returns the time taken for the TLS handshake of the traced request.
// in error cases the time will be set to -1.
func (t *requestTracer) TlsTime() float64 {
	return timeDelta(t.tTLSStart.Load(), t.tTLSDone.Load())
}

// traceRequest attaches a httptrace.ClientTrace to the given request. The
// returned requestTracer indicates whether certain stages of the requests
// lifecycle have been reached.
func traceRequest(req *http.Request) (*http.Request, *requestTracer) {
	t := &requestTracer{}
	r2 := req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			t.gotConn.Store(true)
			t.connInfo.Store(&info)
			if !info.Reused {
				// FIXME: workaround for https://github.com/golang/go/issues/59310
				// This gives net/http: Transport.persistConn.readLoop the time possibly mark the connection
				// as broken before roundtrip starts.
				time.Sleep(500 * time.Microsecond)
			}
		},
		DNSStart: func(_ httptrace.DNSStartInfo) {
			t.tDNSStart.Store(time.Now().UnixNano())
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			t.tDNSDone.Store(time.Now().UnixNano())
		},
		ConnectStart: func(_, _ string) {
			t.tDialStart.Store(time.Now().UnixNano())
		},
		ConnectDone: func(_, _ string, _ error) {
			t.tDialDone.Store(time.Now().UnixNano())
		},
		TLSHandshakeStart: func() {
			t.tTLSStart.Store(time.Now().UnixNano())
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			t.tTLSDone.Store(time.Now().UnixNano())
		},
		WroteHeaders: func() {
			t.wroteHeaders.Store(true)
		},
	}))
	return r2, t
}
