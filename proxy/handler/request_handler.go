package handler

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/errorwriter"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"
	"github.com/uber-go/zap"
)

var NoEndpointsAvailable = errors.New("No endpoints available")

type RequestHandler struct {
	logger      logger.Logger
	errorWriter errorwriter.ErrorWriter
	reporter    metrics.ProxyReporter

	request  *http.Request
	response utils.ProxyResponseWriter

	endpointDialTimeout  time.Duration
	websocketDialTimeout time.Duration
	maxAttempts          int

	tlsConfigTemplate *tls.Config

	forwarder               *Forwarder
	disableXFFLogging       bool
	disableSourceIPLogging  bool
	hopByHopHeadersToFilter []string
}

func NewRequestHandler(
	request *http.Request,
	response utils.ProxyResponseWriter,
	r metrics.ProxyReporter,
	logger logger.Logger,
	errorWriter errorwriter.ErrorWriter,
	endpointDialTimeout time.Duration,
	websocketDialTimeout time.Duration,
	maxAttempts int,
	tlsConfig *tls.Config,
	hopByHopHeadersToFilter []string,
	opts ...func(*RequestHandler),
) *RequestHandler {
	reqHandler := &RequestHandler{
		errorWriter:             errorWriter,
		reporter:                r,
		request:                 request,
		response:                response,
		endpointDialTimeout:     endpointDialTimeout,
		websocketDialTimeout:    websocketDialTimeout,
		maxAttempts:             maxAttempts,
		tlsConfigTemplate:       tlsConfig,
		hopByHopHeadersToFilter: hopByHopHeadersToFilter,
	}

	for _, option := range opts {
		option(reqHandler)
	}

	requestLogger := setupLogger(reqHandler.disableXFFLogging, reqHandler.disableSourceIPLogging, request, logger)
	reqHandler.forwarder = &Forwarder{
		BackendReadTimeout: websocketDialTimeout,
		Logger:             requestLogger,
	}
	reqHandler.logger = requestLogger

	return reqHandler
}

func setupLogger(disableXFFLogging, disableSourceIPLogging bool, request *http.Request, logger logger.Logger) logger.Logger {
	fields := []zap.Field{
		zap.String("RemoteAddr", request.RemoteAddr),
		zap.String("Host", request.Host),
		zap.String("Path", request.URL.Path),
		zap.Object("X-Forwarded-For", request.Header["X-Forwarded-For"]),
		zap.Object("X-Forwarded-Proto", request.Header["X-Forwarded-Proto"]),
		zap.Object("X-Vcap-Request-Id", request.Header["X-Vcap-Request-Id"]),
	}
	// Specific indexes below is to preserve the schema in the log line
	if disableSourceIPLogging {
		fields[0] = zap.String("RemoteAddr", "-")
	}

	if disableXFFLogging {
		fields[3] = zap.Object("X-Forwarded-For", "-")
	}

	l := logger.Session("request-handler").With(fields...)
	return l
}

func DisableXFFLogging(t bool) func(*RequestHandler) {
	return func(h *RequestHandler) {
		h.disableXFFLogging = t
	}
}

func DisableSourceIPLogging(t bool) func(*RequestHandler) {
	return func(h *RequestHandler) {
		h.disableSourceIPLogging = t
	}
}

func (h *RequestHandler) Logger() logger.Logger {
	return h.logger
}

func (h *RequestHandler) HandleBadGateway(err error, request *http.Request) {
	h.reporter.CaptureBadGateway()

	handlers.AddRouterErrorHeader(h.response, "endpoint_failure")

	h.errorWriter.WriteError(h.response, http.StatusBadGateway, "Registered endpoint failed to handle the request.", h.logger)
	h.response.Done()
}

func (h *RequestHandler) HandleTcpRequest(iter route.EndpointIterator) {
	h.logger.Info("handling-tcp-request", zap.String("Upgrade", "tcp"))

	onConnectionFailed := func(err error) { h.logger.Error("tcp-connection-failed", zap.Error(err)) }
	backendStatusCode, err := h.serveTcp(iter, nil, onConnectionFailed)
	if err != nil {
		h.logger.Error("tcp-request-failed", zap.Error(err))
		h.errorWriter.WriteError(h.response, http.StatusBadGateway, "TCP forwarding to endpoint failed.", h.logger)
		return
	}
	h.response.SetStatus(backendStatusCode)
}

func (h *RequestHandler) HandleWebSocketRequest(iter route.EndpointIterator) {
	h.logger.Info("handling-websocket-request", zap.String("Upgrade", "websocket"))

	onConnectionSucceeded := func(connection net.Conn, endpoint *route.Endpoint) error {
		h.setupRequest(endpoint)
		err := h.request.Write(connection)
		if err != nil {
			return err
		}
		return nil
	}
	onConnectionFailed := func(err error) { h.logger.Error("websocket-connection-failed", zap.Error(err)) }

	backendStatusCode, err := h.serveTcp(iter, onConnectionSucceeded, onConnectionFailed)

	if err != nil {
		h.logger.Error("websocket-request-failed", zap.Error(err))
		h.errorWriter.WriteError(h.response, http.StatusBadGateway, "WebSocket request to endpoint failed.", h.logger)
		h.reporter.CaptureWebSocketFailure()
		return
	}

	h.response.SetStatus(backendStatusCode)
	h.reporter.CaptureWebSocketUpdate()
}

func (h *RequestHandler) SanitizeRequestConnection() {
	if len(h.hopByHopHeadersToFilter) == 0 {
		return
	}
	connections := h.request.Header.Values("Connection")
	for index, connection := range connections {
		if connection != "" {
			values := strings.Split(connection, ",")
			connectionHeader := []string{}
			for i := range values {
				trimmedValue := strings.TrimSpace(values[i])
				found := false
				for _, item := range h.hopByHopHeadersToFilter {
					if strings.EqualFold(item, trimmedValue) {
						found = true
						break
					}
				}
				if !found {
					connectionHeader = append(connectionHeader, trimmedValue)
				}
			}
			h.request.Header[http.CanonicalHeaderKey("Connection")][index] = strings.Join(connectionHeader, ", ")
		}
	}
}

type connSuccessCB func(net.Conn, *route.Endpoint) error
type connFailureCB func(error)

var nilConnSuccessCB = func(net.Conn, *route.Endpoint) error { return nil }
var nilConnFailureCB = func(error) {}

func (h *RequestHandler) serveTcp(
	iter route.EndpointIterator,
	onConnectionSucceeded connSuccessCB,
	onConnectionFailed connFailureCB,
) (int, error) {
	var err error
	var backendConnection net.Conn
	var endpoint *route.Endpoint

	if onConnectionSucceeded == nil {
		onConnectionSucceeded = nilConnSuccessCB
	}
	if onConnectionFailed == nil {
		onConnectionFailed = nilConnFailureCB
	}

	reqInfo, err := handlers.ContextRequestInfo(h.request)
	if err != nil {
		return 0, err
	}
	// httptrace.ClientTrace only works for Transports, so we have to do the tracing manually
	var dialStartedAt, dialFinishedAt, tlsHandshakeStartedAt, tlsHandshakeFinishedAt time.Time

	retry := 0
	for {
		endpoint = iter.Next(retry)
		if endpoint == nil {
			err = NoEndpointsAvailable
			h.HandleBadGateway(err, h.request)
			return 0, err
		}

		iter.PreRequest(endpoint)

		dialStartedAt = time.Now()
		backendConnection, err = net.DialTimeout("tcp", endpoint.CanonicalAddr(), h.endpointDialTimeout)
		dialFinishedAt = time.Now()
		if endpoint.IsTLS() {
			tlsConfigLocal := utils.TLSConfigWithServerName(endpoint.ServerCertDomainSAN, h.tlsConfigTemplate, false)
			tlsBackendConnection := tls.Client(backendConnection, tlsConfigLocal)
			tlsHandshakeStartedAt = time.Now()
			err = tlsBackendConnection.Handshake()
			tlsHandshakeFinishedAt = time.Now()
			backendConnection = tlsBackendConnection
		}

		if err == nil {
			defer iter.PostRequest(endpoint)
			break
		} else {
			iter.PostRequest(endpoint)
		}

		reqInfo.FailedAttempts++
		reqInfo.LastFailedAttemptFinishedAt = time.Now()

		iter.EndpointFailed(err)
		onConnectionFailed(err)

		retry++
		if retry == h.maxAttempts {
			return 0, err
		}
	}
	if backendConnection == nil {
		return 0, nil
	}
	defer backendConnection.Close()

	err = onConnectionSucceeded(backendConnection, endpoint)
	if err != nil {
		return 0, err
	}

	client, _, err := h.hijack()
	if err != nil {
		return 0, err
	}
	defer client.Close()

	// Round trip was successful at this point
	reqInfo.RoundTripSuccessful = true

	// Record the times from the last attempt, but only if it succeeded.
	reqInfo.DialStartedAt = dialStartedAt
	reqInfo.DialFinishedAt = dialFinishedAt
	reqInfo.TlsHandshakeStartedAt = tlsHandshakeStartedAt
	reqInfo.TlsHandshakeFinishedAt = tlsHandshakeFinishedAt

	// Any status code has already been sent to the client,
	// but this is the value that gets written to the access logs
	backendStatusCode, err := h.forwarder.ForwardIO(client, backendConnection)

	// add X-Cf-RouterError header to improve traceability in access log
	if err != nil {
		errMsg := fmt.Sprintf("endpoint_failure (%s)", err.Error())
		handlers.AddRouterErrorHeader(h.response, errMsg)
	}

	return backendStatusCode, nil
}

func (h *RequestHandler) setupRequest(endpoint *route.Endpoint) {
	h.setRequestURL(endpoint.CanonicalAddr())
	h.setRequestXForwardedFor()
	SetRequestXRequestStart(h.request)
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

func SetRequestXRequestStart(request *http.Request) {
	if _, ok := request.Header[http.CanonicalHeaderKey("X-Request-Start")]; !ok {
		request.Header.Set("X-Request-Start", strconv.FormatInt(time.Now().UnixNano()/1e6, 10))
	}
}

func SetRequestXCfInstanceId(request *http.Request, endpoint *route.Endpoint) {
	value := endpoint.PrivateInstanceId
	if value == "" {
		value = endpoint.CanonicalAddr()
	}

	request.Header.Set(router_http.CfInstanceIdHeader, value)
}

func (h *RequestHandler) hijack() (client net.Conn, io *bufio.ReadWriter, err error) {
	return h.response.Hijack()
}
