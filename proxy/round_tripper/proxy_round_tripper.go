package round_tripper

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/uber-go/zap"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/proxy/handler"
	"code.cloudfoundry.org/gorouter/route"
)

const (
	VcapCookieId        = "__VCAP_ID__"
	StickyCookieKey     = "JSESSIONID"
	CookieHeader        = "Set-Cookie"
	BadGatewayMessage   = "502 Bad Gateway: Registered endpoint failed to handle the request."
	SSLHandshakeMessage = "525 SSL Handshake Failed"
)

//go:generate counterfeiter -o fakes/fake_proxy_round_tripper.go . ProxyRoundTripper
type ProxyRoundTripper interface {
	http.RoundTripper
	CancelRequest(*http.Request)
}

type AfterRoundTrip func(req *http.Request, rsp *http.Response, endpoint *route.Endpoint, err error)

func NewProxyRoundTripper(
	transport ProxyRoundTripper,
	logger logger.Logger,
	traceKey string,
	routerIP string,
	defaultLoadBalance string,
	combinedReporter metrics.CombinedReporter,
	secureCookies bool,
	localPort uint16,
) ProxyRoundTripper {
	return &roundTripper{
		logger:             logger,
		transport:          transport,
		traceKey:           traceKey,
		routerIP:           routerIP,
		defaultLoadBalance: defaultLoadBalance,
		combinedReporter:   combinedReporter,
		secureCookies:      secureCookies,
		localPort:          localPort,
	}
}

type roundTripper struct {
	transport          ProxyRoundTripper
	logger             logger.Logger
	traceKey           string
	routerIP           string
	defaultLoadBalance string
	combinedReporter   metrics.CombinedReporter
	secureCookies      bool
	localPort          uint16
}

func (rt *roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	var err error
	var res *http.Response
	var endpoint *route.Endpoint

	if request.Body != nil {
		closer := request.Body
		request.Body = ioutil.NopCloser(request.Body)
		defer func() {
			closer.Close()
		}()
	}

	reqInfo, err := handlers.ContextRequestInfo(request)
	if err != nil {
		return nil, err
	}
	if reqInfo.RoutePool == nil {
		return nil, errors.New("RoutePool not set on context")
	}

	if reqInfo.ProxyResponseWriter == nil {
		return nil, errors.New("ProxyResponseWriter not set on context")
	}

	stickyEndpointID := getStickySession(request)
	iter := reqInfo.RoutePool.Endpoints(rt.defaultLoadBalance, stickyEndpointID)

	logger := rt.logger
	for retry := 0; retry < handler.MaxRetries; retry++ {
		logger = rt.logger

		if reqInfo.RouteServiceURL == nil {
			endpoint, err = rt.selectEndpoint(iter, request)
			if err != nil {
				break
			}
			logger = logger.With(zap.Nest("route-endpoint", endpoint.ToLogData()...))

			logger.Debug("backend", zap.Int("attempt", retry))
			if endpoint.IsTLS() {
				request.URL.Scheme = "https"
			}
			res, err = rt.backendRoundTrip(request, endpoint, iter)
			if err == nil || !retryableError(err) {
				break
			}
			iter.EndpointFailed()
			logger.Error("backend-endpoint-failed", zap.Error(err))
		} else {
			logger.Debug(
				"route-service",
				zap.Object("route-service-url", reqInfo.RouteServiceURL),
				zap.Int("attempt", retry),
			)

			endpoint = newRouteServiceEndpoint()
			request.Host = reqInfo.RouteServiceURL.Host
			request.URL = new(url.URL)
			*request.URL = *reqInfo.RouteServiceURL
			if reqInfo.IsInternalRouteService {
				// note: this *looks* like it breaks TLS to internal route service backends,
				// but in fact it is right!  this hairpins back on the gorouter, and the subsequent
				// request from the gorouter will go to a backend using TLS (if tls_port is set on that endpoint)
				request.URL.Scheme = "http"
				request.URL.Host = fmt.Sprintf("localhost:%d", rt.localPort)
			}

			res, err = rt.transport.RoundTrip(request)
			if err == nil {
				if res != nil && (res.StatusCode < 200 || res.StatusCode >= 300) {
					logger.Info(
						"route-service-response",
						zap.String("endpoint", request.URL.String()),
						zap.Int("status-code", res.StatusCode),
					)
				}
				break
			}
			if !retryableError(err) {
				break
			}
			logger.Error("route-service-connection-failed", zap.Error(err))
		}
	}

	reqInfo.RouteEndpoint = endpoint
	reqInfo.StoppedAt = time.Now()

	if err != nil {
		responseWriter := reqInfo.ProxyResponseWriter
		responseWriter.Header().Set(router_http.CfRouterError, "endpoint_failure")

		if _, ok := err.(tls.RecordHeaderError); ok {
			http.Error(responseWriter, SSLHandshakeMessage, 525)
		} else {
			http.Error(responseWriter, BadGatewayMessage, http.StatusBadGateway)
		}
		logger.Error("endpoint-failed", zap.Error(err))
		responseWriter.Header().Del("Connection")

		rt.combinedReporter.CaptureBadGateway()

		responseWriter.Done()

		return nil, err
	}

	if rt.traceKey != "" && request.Header.Get(router_http.VcapTraceHeader) == rt.traceKey {
		if res != nil && endpoint != nil {
			res.Header.Set(router_http.VcapRouterHeader, rt.routerIP)
			res.Header.Set(router_http.VcapBackendHeader, endpoint.CanonicalAddr())
			res.Header.Set(router_http.CfRouteEndpointHeader, endpoint.CanonicalAddr())
		}
	}

	if res != nil && endpoint.PrivateInstanceId != "" {
		setupStickySession(
			res, endpoint, stickyEndpointID, rt.secureCookies,
			reqInfo.RoutePool.ContextPath(),
		)
	}

	return res, nil
}

func (rt *roundTripper) CancelRequest(request *http.Request) {
	rt.transport.CancelRequest(request)
}

func (rt *roundTripper) backendRoundTrip(
	request *http.Request,
	endpoint *route.Endpoint,
	iter route.EndpointIterator,
) (*http.Response, error) {
	request.URL.Host = endpoint.CanonicalAddr()
	request.Header.Set("X-CF-ApplicationID", endpoint.ApplicationId)
	request.Header.Set("X-CF-InstanceIndex", endpoint.PrivateInstanceIndex)
	handler.SetRequestXCfInstanceId(request, endpoint)

	// increment connection stats
	iter.PreRequest(endpoint)

	rt.combinedReporter.CaptureRoutingRequest(endpoint)
	res, err := rt.transport.RoundTrip(request)

	// decrement connection stats
	iter.PostRequest(endpoint)
	return res, err
}

func (rt *roundTripper) selectEndpoint(iter route.EndpointIterator, request *http.Request) (*route.Endpoint, error) {
	endpoint := iter.Next()
	if endpoint == nil {
		return nil, handler.NoEndpointsAvailable
	}

	return endpoint, nil
}

func setupStickySession(
	response *http.Response,
	endpoint *route.Endpoint,
	originalEndpointId string,
	secureCookies bool,
	path string,
) {
	secure := false
	maxAge := 0

	// did the endpoint change?
	sticky := originalEndpointId != "" && originalEndpointId != endpoint.PrivateInstanceId

	for _, v := range response.Cookies() {
		if v.Name == StickyCookieKey {
			sticky = true
			if v.MaxAge < 0 {
				maxAge = v.MaxAge
			}
			secure = v.Secure
			break
		}
	}

	if sticky {
		// right now secure attribute would as equal to the JSESSION ID cookie (if present),
		// but override if set to true in config
		if secureCookies {
			secure = true
		}

		cookie := &http.Cookie{
			Name:     VcapCookieId,
			Value:    endpoint.PrivateInstanceId,
			Path:     path,
			MaxAge:   maxAge,
			HttpOnly: true,
			Secure:   secure,
		}

		if v := cookie.String(); v != "" {
			response.Header.Add(CookieHeader, v)
		}
	}
}

func getStickySession(request *http.Request) string {
	// Try choosing a backend using sticky session
	if _, err := request.Cookie(StickyCookieKey); err == nil {
		if sticky, err := request.Cookie(VcapCookieId); err == nil {
			return sticky.Value
		}
	}
	return ""
}

func retryableError(err error) bool {
	ne, netErr := err.(*net.OpError)
	if netErr && (ne.Op == "dial" || ne.Op == "read" && ne.Err.Error() == "read: connection reset by peer") {
		return true
	}
	return false
}

func newRouteServiceEndpoint() *route.Endpoint {
	return &route.Endpoint{
		Tags: map[string]string{},
	}
}
