package router

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	steno "github.com/cloudfoundry/gosteno"

	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/route"
)

const (
	VcapBackendHeader     = "X-Vcap-Backend"
	CfRouteEndpointHeader = "X-Cf-RouteEndpoint"
	VcapRouterHeader      = "X-Vcap-Router"
	VcapTraceHeader       = "X-Vcap-Trace"

	VcapCookieId    = "__VCAP_ID__"
	StickyCookieKey = "JSESSIONID"
)

type Proxy struct {
	sync.RWMutex
	*steno.Logger
	*config.Config
	*registry.Registry
	Varz
	*AccessLogger
}

type responseWriter struct {
	http.ResponseWriter
	*steno.Logger
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj := rw.ResponseWriter.(http.Hijacker)
	return hj.Hijack()
}

func (rw *responseWriter) WriteStatus(code int) {
	body := fmt.Sprintf("%d %s", code, http.StatusText(code))
	rw.Warn(body)
	http.Error(rw, body, code)
}

func (rw *responseWriter) CopyFrom(src io.Reader) (int64, error) {
	if src == nil {
		return 0, nil
	}

	var dst io.Writer = rw

	// Use MaxLatencyFlusher if needed
	if v, ok := rw.ResponseWriter.(writeFlusher); ok {
		u := NewMaxLatencyWriter(v, 50*time.Millisecond)
		defer u.Stop()
		dst = u
	}

	return io.Copy(dst, src)
}

func NewProxy(c *config.Config, r *registry.Registry, v Varz) *Proxy {
	p := &Proxy{
		Config:   c,
		Logger:   steno.NewLogger("router.proxy"),
		Registry: r,
		Varz:     v,
	}

	if c.AccessLog != "" {
		f, err := os.OpenFile(c.AccessLog, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
		if err != nil {
			panic(err)
		}

		p.AccessLogger = NewAccessLogger(f)
		go p.AccessLogger.Run()
	}

	return p
}

func hostWithoutPort(req *http.Request) string {
	host := req.Host

	// Remove :<port>
	pos := strings.Index(host, ":")
	if pos >= 0 {
		host = host[0:pos]
	}

	return host
}

func (proxy *Proxy) Lookup(request *http.Request) (*route.Endpoint, bool) {
	host := hostWithoutPort(request)

	// Try choosing a backend using sticky session
	if _, err := request.Cookie(StickyCookieKey); err == nil {
		if sticky, err := request.Cookie(VcapCookieId); err == nil {
			routeEndpoint, ok := proxy.Registry.LookupByPrivateInstanceId(host, sticky.Value)
			if ok {
				return routeEndpoint, ok
			}
		}
	}

	// Choose backend using host alone
	return proxy.Registry.Lookup(host)
}

func (proxy *Proxy) ServeHTTP(httpResponseWriter http.ResponseWriter, request *http.Request) {
	responseWriter := responseWriter{
		ResponseWriter: httpResponseWriter,
		Logger:         proxy.Logger.Copy(),
	}

	responseWriter.Set("RemoteAddr", request.RemoteAddr)
	responseWriter.Set("Host", request.Host)
	responseWriter.Set("X-Forwarded-For", request.Header["X-Forwarded-For"])
	responseWriter.Set("X-Forwarded-Proto", request.Header["X-Forwarded-Proto"])

	accessLog := AccessLogRecord{
		Request:   request,
		StartedAt: time.Now(),
	}

	if request.ProtoMajor != 1 && (request.ProtoMinor != 0 || request.ProtoMinor != 1) {
		c, brw, err := responseWriter.Hijack()
		if err != nil {
			panic(err)
		}

		fmt.Fprintf(brw, "HTTP/1.0 400 Bad Request\r\n\r\n")
		brw.Flush()
		c.Close()
		return
	}

	start := time.Now()

	// Return 200 OK for heartbeats from LB
	if request.UserAgent() == "HTTP-Monitor/1.1" {
		responseWriter.WriteHeader(http.StatusOK)
		fmt.Fprintln(responseWriter, "ok")
		return
	}

	routeEndpoint, ok := proxy.Lookup(request)
	if !ok {
		proxy.Varz.CaptureBadRequest(request)
		responseWriter.WriteStatus(http.StatusNotFound)
		return
	}

	responseWriter.Set("RouteEndpoint", routeEndpoint.ToLogData())
	responseWriter.Set("Backend", routeEndpoint.ToLogData()) // Deprecated: Use RouteEndpoint

	accessLog.RouteEndpoint = routeEndpoint

	proxy.Registry.CaptureRoutingRequest(routeEndpoint, start)
	proxy.Varz.CaptureRoutingRequest(routeEndpoint, request)

	request.URL.Scheme = "http"
	request.URL.Host = routeEndpoint.CanonicalAddr()

	// Add X-Forwarded-For
	if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
		// We assume there is a trusted upstream (L7 LB) that properly
		// strips client's XFF header

		// This is sloppy but fine since we don't share this request or
		// headers. Otherwise we should copy the underlying header and
		// append
		xForwardFor := append(request.Header["X-Forwarded-For"], host)
		request.Header.Set("X-Forwarded-For", strings.Join(xForwardFor, ", "))
	}

	// Check if the connection is going to be upgraded to a raw TCP connection
	if checkTcpUpgrade(responseWriter, request) {
		serveTcp(responseWriter, request)
		return
	}

	// Check if the connection is going to be upgraded to a WebSocket connection
	if checkWebSocketUpgrade(responseWriter, request) {
		serveWebSocket(responseWriter, request)
		return
	}

	// Use a new connection for every request
	// Keep-alive can be bolted on later, if we want to
	request.Close = true
	request.Header.Del("Connection")

	roundTripResponse, err := http.DefaultTransport.RoundTrip(request)

	latency := time.Since(start)

	accessLog.FirstByteAt = time.Now()
	accessLog.Response = roundTripResponse

	if err != nil {
		proxy.Varz.CaptureRoutingResponse(routeEndpoint, roundTripResponse, latency)
		responseWriter.Warnf("Error reading from upstream: %s", err)
		responseWriter.WriteStatus(http.StatusBadGateway)
		return
	}

	proxy.Varz.CaptureRoutingResponse(routeEndpoint, roundTripResponse, latency)

	for k, vv := range roundTripResponse.Header {
		for _, v := range vv {
			responseWriter.Header().Add(k, v)
		}
	}

	if proxy.Config.TraceKey != "" && request.Header.Get(VcapTraceHeader) == proxy.Config.TraceKey {
		responseWriter.Header().Set(VcapRouterHeader, proxy.Config.Ip)
		responseWriter.Header().Set(VcapBackendHeader, routeEndpoint.CanonicalAddr())
		responseWriter.Header().Set(CfRouteEndpointHeader, routeEndpoint.CanonicalAddr())
	}

	needSticky := false
	for _, v := range roundTripResponse.Cookies() {
		if v.Name == StickyCookieKey {
			needSticky = true
			break
		}
	}

	if needSticky && routeEndpoint.PrivateInstanceId != "" {
		cookie := &http.Cookie{
			Name:  VcapCookieId,
			Value: routeEndpoint.PrivateInstanceId,
			Path:  "/",
		}
		http.SetCookie(responseWriter, cookie)
	}

	responseWriter.WriteHeader(roundTripResponse.StatusCode)
	bytesSent, _ := responseWriter.CopyFrom(roundTripResponse.Body)

	accessLog.FinishedAt = time.Now()
	accessLog.BodyBytesSent = bytesSent

	if proxy.AccessLogger != nil {
		proxy.AccessLogger.Log(accessLog)
	}
}

func checkWebSocketUpgrade(rw http.ResponseWriter, req *http.Request) bool {
	return connectionUpgrade(rw, req) == "websocket"
}

func checkTcpUpgrade(rw http.ResponseWriter, req *http.Request) bool {
	return connectionUpgrade(rw, req) == "tcp"
}

func connectionUpgrade(rw http.ResponseWriter, req *http.Request) string {
	if req.Header.Get("Connection") == "Upgrade" {
		return req.Header.Get("Upgrade")
	} else {
		return ""
	}
}

func serveTcp(rw responseWriter, req *http.Request) {
	var err error

	rw.Set("Upgrade", "tcp")

	client, connection, err := hijackRequest(rw, req.URL.Host)
	if err != nil {
		rw.Warnf("Request hijack failed: %s", err)
		rw.WriteStatus(http.StatusBadRequest)
		return
	}

	defer client.Close()
	defer connection.Close()

	forwardIO(client, connection)
}

func serveWebSocket(rw responseWriter, req *http.Request) {
	var err error

	rw.Set("Upgrade", "websocket")

	client, connection, err := hijackRequest(rw, req.URL.Host)
	if err != nil {
		rw.Warnf("Request hijack failed: %s", err)
		rw.WriteStatus(http.StatusBadRequest)
		return
	}

	defer client.Close()
	defer connection.Close()

	// Write request
	err = req.Write(connection)
	if err != nil {
		rw.Warnf("Writing request: %s", err)
		rw.WriteStatus(http.StatusBadRequest)
		return
	}

	forwardIO(client, connection)
}

func hijackRequest(rw responseWriter, addr string) (client, connection net.Conn, err error) {
	client, _, err = rw.Hijack()
	if err != nil {
		return
	}

	connection, err = net.Dial("tcp", addr)
	if err != nil {
		return
	}

	return
}

func forwardIO(a, b net.Conn) {
	done := make(chan bool, 2)

	copy := func(dst io.Writer, src io.Reader) {
		// don't care about errors here
		io.Copy(dst, src)
		done <- true
	}

	go copy(a, b)
	go copy(b, a)

	<-done
}
