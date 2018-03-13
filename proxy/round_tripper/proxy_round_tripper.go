package round_tripper

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/uber-go/zap"

	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/proxy/fails"
	"code.cloudfoundry.org/gorouter/proxy/handler"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"
)

const (
	VcapCookieId              = "__VCAP_ID__"
	StickyCookieKey           = "JSESSIONID"
	CookieHeader              = "Set-Cookie"
	BadGatewayMessage         = "502 Bad Gateway: Registered endpoint failed to handle the request."
	HostnameErrorMessage      = "503 Service Unavailable"
	InvalidCertificateMessage = "526 Invalid SSL Certificate"
	SSLHandshakeMessage       = "525 SSL Handshake Failed"
	SSLCertRequiredMessage    = "496 SSL Certificate Required"
	ContextCancelledMessage   = "499 Request Cancelled"
)

//go:generate counterfeiter -o fakes/fake_proxy_round_tripper.go . ProxyRoundTripper
type ProxyRoundTripper interface {
	http.RoundTripper
	CancelRequest(*http.Request)
}

type RoundTripperFactory interface {
	New(expectedServerName string) ProxyRoundTripper
}

func GetRoundTripper(e *route.Endpoint, roundTripperFactory RoundTripperFactory) ProxyRoundTripper {
	e.Lock()
	if e.RoundTripper == nil {

		e.RoundTripper = roundTripperFactory.New(e.ServerCertDomainSAN)
	}
	e.Unlock()

	return e.RoundTripper
}

//go:generate counterfeiter -o fakes/fake_error_handler.go --fake-name ErrorHandler . errorHandler
type errorHandler interface {
	HandleError(utils.ProxyResponseWriter, error)
}

type AfterRoundTrip func(req *http.Request, rsp *http.Response, endpoint *route.Endpoint, err error)

func NewProxyRoundTripper(
	roundTripperFactory RoundTripperFactory,
	retryableClassifier fails.Classifier,
	logger logger.Logger,
	defaultLoadBalance string,
	combinedReporter metrics.ProxyReporter,
	secureCookies bool,
	localPort uint16,
	errorHandler errorHandler,
	routeServicesClient http.RoundTripper,
	endpointTimeout time.Duration,
) ProxyRoundTripper {
	return &roundTripper{
		logger:              logger,
		defaultLoadBalance:  defaultLoadBalance,
		combinedReporter:    combinedReporter,
		secureCookies:       secureCookies,
		localPort:           localPort,
		roundTripperFactory: roundTripperFactory,
		retryableClassifier: retryableClassifier,
		errorHandler:        errorHandler,
		routeServicesClient: routeServicesClient,
		endpointTimeout:     endpointTimeout,
	}
}

type roundTripper struct {
	logger              logger.Logger
	defaultLoadBalance  string
	combinedReporter    metrics.ProxyReporter
	secureCookies       bool
	localPort           uint16
	roundTripperFactory RoundTripperFactory
	retryableClassifier fails.Classifier
	errorHandler        errorHandler
	routeServicesClient http.RoundTripper
	endpointTimeout     time.Duration
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
	var selectEndpointErr error
	for retry := 0; retry < handler.MaxRetries; retry++ {
		logger = rt.logger

		if reqInfo.RouteServiceURL == nil {
			endpoint, selectEndpointErr = rt.selectEndpoint(iter, request)
			if selectEndpointErr != nil {
				break
			}
			logger = logger.With(zap.Nest("route-endpoint", endpoint.ToLogData()...))
			reqInfo.RouteEndpoint = endpoint

			logger.Debug("backend", zap.Int("attempt", retry))
			if endpoint.IsTLS() {
				request.URL.Scheme = "https"
			}
			res, err = rt.backendRoundTrip(request, endpoint, iter)
			if err == nil || !rt.retryableClassifier.Classify(err) {
				break
			}
			iter.EndpointFailed(err)
			logger.Error("backend-endpoint-failed", zap.Error(err))
		} else {
			logger.Debug(
				"route-service",
				zap.Object("route-service-url", reqInfo.RouteServiceURL),
				zap.Int("attempt", retry),
			)

			endpoint = newRouteServiceEndpoint()
			reqInfo.RouteEndpoint = endpoint
			request.Host = reqInfo.RouteServiceURL.Host
			request.URL = new(url.URL)
			*request.URL = *reqInfo.RouteServiceURL

			var tr http.RoundTripper
			tr = GetRoundTripper(endpoint, rt.roundTripperFactory)
			if reqInfo.IsInternalRouteService {
				// note: this *looks* like it breaks TLS to internal route service backends,
				// but in fact it is right!  this hairpins back on the gorouter, and the subsequent
				// request from the gorouter will go to a backend using TLS (if tls_port is set on that endpoint)
				tr = rt.routeServicesClient
			}
			res, err = rt.timedRoundTrip(tr, request)
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
			if !rt.retryableClassifier.Classify(err) {
				break
			}
			logger.Error("route-service-connection-failed", zap.Error(err))
		}
	}

	reqInfo.StoppedAt = time.Now()

	finalErr := err
	if finalErr == nil {
		finalErr = selectEndpointErr
	}

	if finalErr != nil {
		rt.errorHandler.HandleError(reqInfo.ProxyResponseWriter, finalErr)
		logger.Error("endpoint-failed", zap.Error(finalErr))
		return nil, finalErr
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
	endpoint, err := handlers.GetEndpoint(request.Context())
	if err != nil {
		return
	}

	tr := GetRoundTripper(endpoint, rt.roundTripperFactory)
	tr.CancelRequest(request)
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
	tr := GetRoundTripper(endpoint, rt.roundTripperFactory)
	res, err := rt.timedRoundTrip(tr, request)

	// decrement connection stats
	iter.PostRequest(endpoint)
	return res, err
}

func (rt *roundTripper) timedRoundTrip(tr http.RoundTripper, request *http.Request) (*http.Response, error) {

	if rt.endpointTimeout <= 0 {
		return tr.RoundTrip(request)
	}
	reqCtx, cancel := context.WithTimeout(request.Context(), rt.endpointTimeout)
	request = request.WithContext(reqCtx)
	errored := make(chan struct{}, 1)

	go func() {
		defer cancel()
		select {
		case <-reqCtx.Done():
		case <-errored:
		}
	}()

	resp, err := tr.RoundTrip(request)
	if err != nil {
		cancel()
		errored <- struct{}{}
	}

	return resp, err
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

func newRouteServiceEndpoint() *route.Endpoint {
	return &route.Endpoint{
		Tags: map[string]string{},
	}
}
