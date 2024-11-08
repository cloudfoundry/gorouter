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
	connReused   atomic.Bool
	wroteHeaders atomic.Bool
	localAddr    atomic.Pointer[string]

	// all times are stored as returned by time.Time{}.UnixNano()
	dnsStart  atomic.Int64
	dnsDone   atomic.Int64
	dialStart atomic.Int64
	dialDone  atomic.Int64
	tlsStart  atomic.Int64
	tlsDone   atomic.Int64
}

// Reset the trace data. Helpful when performing the same request again.
func (t *requestTracer) Reset() {
	t.gotConn.Store(false)
	t.connReused.Store(false)
	t.wroteHeaders.Store(false)
	t.localAddr.Store(nil)
	t.dnsStart.Store(0)
	t.dnsDone.Store(0)
	t.dialStart.Store(0)
	t.dialDone.Store(0)
	t.tlsStart.Store(0)
	t.tlsDone.Store(0)
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
	return t.connReused.Load()
}

func (t *requestTracer) LocalAddr() string {
	p := t.localAddr.Load()
	if p == nil {
		return ""
	}
	return *p
}

func (t *requestTracer) DnsStart() time.Time {
	return time.Unix(0, t.dnsStart.Load())
}

func (t *requestTracer) DnsDone() time.Time {
	return time.Unix(0, t.dnsDone.Load())
}

func (t *requestTracer) DialStart() time.Time {
	return time.Unix(0, t.dialStart.Load())
}

func (t *requestTracer) DialDone() time.Time {
	return time.Unix(0, t.dialDone.Load())
}

func (t *requestTracer) TlsStart() time.Time {
	return time.Unix(0, t.tlsStart.Load())
}

func (t *requestTracer) TlsDone() time.Time {
	return time.Unix(0, t.tlsDone.Load())
}

// DnsTime returns the time taken for the DNS lookup of the traced request.
// If the time can't be calculated -1 is returned.
func (t *requestTracer) DnsTime() float64 {
	s := t.DnsDone().Sub(t.DnsStart()).Seconds()
	if s < 0 {
		return -1
	} else {
		return s
	}
}

// DialTime returns the time taken for the TCP handshake of the traced request.
// If the time can't be calculated -1 is returned.
func (t *requestTracer) DialTime() float64 {
	s := t.DialDone().Sub(t.DialStart()).Seconds()
	if s < 0 {
		return -1
	} else {
		return s
	}
}

// TlsTime returns the time taken for the TLS handshake of the traced request.
// If the time can't be calculated -1 is returned.
func (t *requestTracer) TlsTime() float64 {
	s := t.TlsDone().Sub(t.TlsStart()).Seconds()
	if s < 0 {
		return -1
	} else {
		return s
	}
}

// traceRequest attaches a httptrace.ClientTrace to the given request. The
// returned requestTracer indicates whether certain stages of the requests
// lifecycle have been reached.
func traceRequest(req *http.Request) (*http.Request, *requestTracer) {
	t := &requestTracer{}
	r2 := req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			t.gotConn.Store(true)
			t.connReused.Store(info.Reused)
			la := info.Conn.LocalAddr().String()
			t.localAddr.Store(&la)

			// FIXME: due to https://github.com/golang/go/issues/31259 this breaks our acceptance tests and is dangerous
			//        disabled for now even though this will reduce the number of requests we can retry
			// if !info.Reused {
			//	// FIXME: workaround for https://github.com/golang/go/issues/59310
			//	// This gives net/http: Transport.persistConn.readLoop the time possibly mark the connection
			//	// as broken before roundtrip starts.
			//	time.Sleep(500 * time.Microsecond)
			// }
		},
		DNSStart: func(_ httptrace.DNSStartInfo) {
			t.dnsStart.Store(time.Now().UnixNano())
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			t.dnsDone.Store(time.Now().UnixNano())
		},
		ConnectStart: func(_, _ string) {
			t.dialStart.Store(time.Now().UnixNano())
		},
		ConnectDone: func(_, _ string, _ error) {
			t.dialDone.Store(time.Now().UnixNano())
		},
		TLSHandshakeStart: func() {
			t.tlsStart.Store(time.Now().UnixNano())
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			t.tlsDone.Store(time.Now().UnixNano())
		},
		WroteHeaders: func() {
			t.wroteHeaders.Store(true)
		},
	}))
	return r2, t
}
