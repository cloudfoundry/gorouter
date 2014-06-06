package proxy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudfoundry/gorouter/common"
	router_http "github.com/cloudfoundry/gorouter/common/http"
	"github.com/cloudfoundry/gorouter/route"
	steno "github.com/cloudfoundry/gosteno"
)

type RequestHandler struct {
	logger *steno.Logger

	request  *http.Request
	response http.ResponseWriter
}

func NewRequestHandler(request *http.Request, response http.ResponseWriter) RequestHandler {
	return RequestHandler{
		logger: createLogger(request),

		request:  request,
		response: response,
	}
}

func createLogger(request *http.Request) *steno.Logger {
	logger := steno.NewLogger("router.proxy.request-handler")

	logger.Set("RemoteAddr", request.RemoteAddr)
	logger.Set("Host", request.Host)
	logger.Set("Path", request.URL.Path)
	logger.Set("X-Forwarded-For", request.Header["X-Forwarded-For"])
	logger.Set("X-Forwarded-Proto", request.Header["X-Forwarded-Proto"])

	return logger
}

func (h *RequestHandler) HandleHeartbeat() {
	h.response.WriteHeader(http.StatusOK)
	h.response.Write([]byte("ok\n"))
	h.request.Close = true
}

func (h *RequestHandler) HandleUnsupportedProtocol() {
	// must be hijacked, otherwise no response is sent back
	conn, buf, err := h.hijack()
	if err != nil {
		h.writeStatus(http.StatusBadRequest, "Unsupported protocol")
		return
	}

	fmt.Fprintf(buf, "HTTP/1.0 400 Bad Request\r\n\r\n")
	buf.Flush()
	conn.Close()
}

func (h *RequestHandler) HandleMissingRoute() {
	h.logger.Warnf("proxy.endpoint.not-found")

	h.response.Header().Set("X-Cf-RouterError", "unknown_route")
	message := fmt.Sprintf("Requested route ('%s') does not exist.", h.request.Host)
	h.writeStatus(http.StatusNotFound, message)
}

func (h *RequestHandler) HandleBadGateway(err error) {
	h.logger.Set("Error", err.Error())
	h.logger.Warnf("proxy.endpoint.failed")

	h.response.Header().Set("X-Cf-RouterError", "endpoint_failure")
	h.writeStatus(http.StatusBadGateway, "Registered endpoint failed to handle the request.")
}

func (h *RequestHandler) HandleTcpRequest(endpoint *route.Endpoint) {
	h.logger.Set("Upgrade", "tcp")

	err := h.serveTcp(endpoint)
	if err != nil {
		h.logger.Set("Error", err.Error())
		h.logger.Warn("proxy.tcp.failed")

		h.writeStatus(http.StatusBadRequest, "TCP forwarding to endpoint failed.")
	}
}

func (h *RequestHandler) HandleWebSocketRequest(endpoint *route.Endpoint) {
	h.setupRequest(endpoint)

	h.logger.Set("Upgrade", "websocket")

	err := h.serveWebSocket(endpoint)
	if err != nil {
		h.logger.Set("Error", err.Error())
		h.logger.Warn("proxy.websocket.failed")

		h.writeStatus(http.StatusBadRequest, "WebSocket request to endpoint failed.")
	}
}

func (h *RequestHandler) writeStatus(code int, message string) {
	body := fmt.Sprintf("%d %s: %s", code, http.StatusText(code), message)

	h.logger.Warn(body)

	http.Error(h.response, body, code)
	if code > 299 {
		h.response.Header().Del("Connection")
	}
}

func (h *RequestHandler) serveTcp(endpoint *route.Endpoint) error {
	var err error

	client, _, err := h.hijack()
	if err != nil {
		return err
	}

	connection, err := net.DialTimeout("tcp", endpoint.CanonicalAddr(), 5*time.Second)
	if err != nil {
		return err
	}

	defer func() {
		client.Close()
		connection.Close()
	}()

	forwardIO(client, connection)

	return nil
}

func (h *RequestHandler) serveWebSocket(endpoint *route.Endpoint) error {
	var err error

	client, _, err := h.hijack()
	if err != nil {
		return err
	}

	connection, err := net.DialTimeout("tcp", endpoint.CanonicalAddr(), 5*time.Second)
	if err != nil {
		return err
	}

	defer func() {
		client.Close()
		connection.Close()
	}()

	err = h.request.Write(connection)
	if err != nil {
		return err
	}

	forwardIO(client, connection)

	return nil
}

func (h *RequestHandler) setupRequest(endpoint *route.Endpoint) {
	h.setRequestURL(endpoint.CanonicalAddr())
	h.setRequestXForwardedFor()
	setRequestXRequestStart(h.request)
	setRequestXVcapRequestId(h.request, h.logger)
}

func (h *RequestHandler) setRequestURL(addr string) {
	h.request.URL.Scheme = "http"
	h.request.URL.Host = addr
}

func (h *RequestHandler) setRequestXForwardedFor() {
	if clientIP, _, err := net.SplitHostPort(h.request.RemoteAddr); err == nil {
		// If we aren't the first proxy retain prior
		// X-Forwarded-For information as a comma+space
		// separated list and fold multiple headers into one.
		if prior, ok := h.request.Header["X-Forwarded-For"]; ok {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		h.request.Header.Set("X-Forwarded-For", clientIP)
	}
}

func setRequestXRequestStart(request *http.Request) {
	if _, ok := request.Header[http.CanonicalHeaderKey("X-Request-Start")]; !ok {
		request.Header.Set("X-Request-Start", strconv.FormatInt(time.Now().UnixNano()/1e6, 10))
	}
}

func setRequestXVcapRequestId(request *http.Request, logger *steno.Logger) {
	uuid, err := common.GenerateUUID()
	if err == nil {
		request.Header.Set(router_http.VcapRequestIdHeader, uuid)
		if logger != nil {
			logger.Set(router_http.VcapRequestIdHeader, uuid)
		}
	}
}

func (h *RequestHandler) hijack() (client net.Conn, io *bufio.ReadWriter, err error) {
	hijacker, ok := h.response.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer cannot hijack")
	}

	return hijacker.Hijack()
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
