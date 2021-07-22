package round_tripper

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"code.cloudfoundry.org/gorouter/routeservice"

	"github.com/uber-go/zap"

	"code.cloudfoundry.org/gorouter/config"
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
	CookieHeader              = "Set-Cookie"
	BadGatewayMessage         = "502 Bad Gateway: Registered endpoint failed to handle the request."
	HostnameErrorMessage      = "503 Service Unavailable"
	InvalidCertificateMessage = "526 Invalid SSL Certificate"
	SSLHandshakeMessage       = "525 SSL Handshake Failed"
	SSLCertRequiredMessage    = "496 SSL Certificate Required"
	ContextCancelledMessage   = "499 Request Cancelled"
	HTTP2Protocol             = "http2"
)

//go:generate counterfeiter -o fakes/fake_proxy_round_tripper.go . ProxyRoundTripper
type ProxyRoundTripper interface {
	http.RoundTripper
	CancelRequest(*http.Request)
}

type RoundTripperFactory interface {
	New(expectedServerName string, isRouteService, isHttp2 bool) ProxyRoundTripper
}

func GetRoundTripper(endpoint *route.Endpoint, roundTripperFactory RoundTripperFactory, isRouteService, http2Enabled bool) ProxyRoundTripper {
	endpoint.RoundTripperInit.Do(func() {
		endpoint.SetRoundTripperIfNil(func() route.ProxyRoundTripper {
			isHttp2 := (endpoint.Protocol == HTTP2Protocol) && http2Enabled
			return roundTripperFactory.New(endpoint.ServerCertDomainSAN, isRouteService, isHttp2)
		})
	})

	return endpoint.RoundTripper()
}

//go:generate counterfeiter -o fakes/fake_error_handler.go --fake-name ErrorHandler . errorHandler
type errorHandler interface {
	HandleError(utils.ProxyResponseWriter, error)
}

func NewProxyRoundTripper(
	roundTripperFactory RoundTripperFactory,
	retriableClassifiers fails.Classifier,
	logger logger.Logger,
	combinedReporter metrics.ProxyReporter,
	errHandler errorHandler,
	routeServicesTransport http.RoundTripper,
	cfg *config.Config,
) ProxyRoundTripper {

	return &roundTripper{
		logger:                   logger,
		defaultLoadBalance:       cfg.LoadBalance,
		combinedReporter:         combinedReporter,
		secureCookies:            cfg.SecureCookies,
		roundTripperFactory:      roundTripperFactory,
		retriableClassifier:      retriableClassifiers,
		errorHandler:             errHandler,
		routeServicesTransport:   routeServicesTransport,
		endpointTimeout:          cfg.EndpointTimeout,
		stickySessionCookieNames: cfg.StickySessionCookieNames,
		http2Enabled:             cfg.EnableHTTP2,
	}
}

type roundTripper struct {
	logger                   logger.Logger
	defaultLoadBalance       string
	combinedReporter         metrics.ProxyReporter
	secureCookies            bool
	roundTripperFactory      RoundTripperFactory
	retriableClassifier      fails.Classifier
	errorHandler             errorHandler
	routeServicesTransport   http.RoundTripper
	endpointTimeout          time.Duration
	stickySessionCookieNames config.StringSet
	http2Enabled             bool
}

func (rt *roundTripper) RoundTrip(originalRequest *http.Request) (*http.Response, error) {
	var err error
	var res *http.Response
	var endpoint *route.Endpoint

	request := originalRequest.Clone(originalRequest.Context())

	if request.Body != nil {
		// Temporarily disable closing of the body while in the RoundTrip function, since
		// the underlying Transport will close the client request body.
		// https://github.com/golang/go/blob/ab5d9f5831cd267e0d8e8954cfe9987b737aec9c/src/net/http/request.go#L179-L182

		request.Body = ioutil.NopCloser(request.Body)
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

	stickyEndpointID := getStickySession(request, rt.stickySessionCookieNames)
	iter := reqInfo.RoutePool.Endpoints(rt.defaultLoadBalance, stickyEndpointID)

	var selectEndpointErr error
	for retry := 0; retry < handler.MaxRetries; retry++ {
		logger := rt.logger

		if reqInfo.RouteServiceURL == nil {
			endpoint, selectEndpointErr = rt.selectEndpoint(iter, request)
			if selectEndpointErr != nil {
				logger.Error("select-endpoint-failed", zap.String("host", reqInfo.RoutePool.Host()), zap.Error(selectEndpointErr))
				break
			}
			logger = logger.With(zap.Nest("route-endpoint", endpoint.ToLogData()...))
			reqInfo.RouteEndpoint = endpoint

			logger.Debug("backend", zap.Int("attempt", retry+1))
			if endpoint.IsTLS() {
				request.URL.Scheme = "https"
			} else {
				request.URL.Scheme = "http"
			}
			res, err = rt.backendRoundTrip(request, endpoint, iter, logger)

			if err != nil {
				// io.EOF errors are considered safe to retry for certain requests
				// Replace the error here to track this state when classifying later.
				if err == io.EOF && isIdempotent(request) {
					err = fails.IdempotentRequestEOFError
				}

				iter.EndpointFailed(err)
				logger.Error("backend-endpoint-failed", zap.Error(err), zap.Int("attempt", retry+1), zap.String("vcap_request_id", request.Header.Get(handlers.VcapRequestIdHeader)))

				if rt.retriableClassifier.Classify(err) {
					logger.Debug("retriable-error", zap.Object("error", err))
					continue
				}
			}

			break
		} else {
			logger.Debug(
				"route-service",
				zap.Object("route-service-url", reqInfo.RouteServiceURL),
				zap.Int("attempt", retry+1),
			)

			endpoint = &route.Endpoint{
				Tags: map[string]string{},
			}
			reqInfo.RouteEndpoint = endpoint
			request.Host = reqInfo.RouteServiceURL.Host
			request.URL = new(url.URL)
			*request.URL = *reqInfo.RouteServiceURL

			var roundTripper http.RoundTripper
			roundTripper = GetRoundTripper(endpoint, rt.roundTripperFactory, true, rt.http2Enabled)
			if reqInfo.ShouldRouteToInternalRouteService {
				roundTripper = rt.routeServicesTransport
			}

			res, err = rt.timedRoundTrip(roundTripper, request, logger)
			if err != nil {
				logger.Error("route-service-connection-failed", zap.Error(err))

				if rt.retriableClassifier.Classify(err) {
					continue
				}
			}

			if res != nil && (res.StatusCode < 200 || res.StatusCode >= 300) {
				logger.Info(
					"route-service-response",
					zap.String("endpoint", request.URL.String()),
					zap.Int("status-code", res.StatusCode),
				)
			}

			break
		}
	}

	reqInfo.StoppedAt = time.Now()

	// if the client disconnects before response is sent then return context.Canceled (499) instead of the gateway error
	if err != nil && originalRequest.Context().Err() == context.Canceled && err != context.Canceled {
		rt.logger.Error("gateway-error-and-original-request-context-cancelled", zap.Error(err))
		err = originalRequest.Context().Err()
	}

	finalErr := err
	if finalErr == nil {
		finalErr = selectEndpointErr
	}

	if finalErr != nil {
		rt.errorHandler.HandleError(reqInfo.ProxyResponseWriter, finalErr)
		return nil, finalErr
	}
	if res != nil && endpoint.PrivateInstanceId != "" && !requestSentToRouteService(request) {
		setupStickySession(
			res, endpoint, stickyEndpointID, rt.secureCookies,
			reqInfo.RoutePool.ContextPath(), rt.stickySessionCookieNames,
		)
	}

	return res, nil
}

func (rt *roundTripper) CancelRequest(request *http.Request) {
	endpoint, err := handlers.GetEndpoint(request.Context())
	if err != nil {
		return
	}

	tr := GetRoundTripper(endpoint, rt.roundTripperFactory, false, rt.http2Enabled)
	tr.CancelRequest(request)
}

func (rt *roundTripper) backendRoundTrip(
	request *http.Request,
	endpoint *route.Endpoint,
	iter route.EndpointIterator,
	logger logger.Logger,
) (*http.Response, error) {
	request.URL.Host = endpoint.CanonicalAddr()
	request.Header.Set("X-CF-ApplicationID", endpoint.ApplicationId)
	request.Header.Set("X-CF-InstanceIndex", endpoint.PrivateInstanceIndex)
	handler.SetRequestXCfInstanceId(request, endpoint)

	// increment connection stats
	iter.PreRequest(endpoint)

	rt.combinedReporter.CaptureRoutingRequest(endpoint)
	tr := GetRoundTripper(endpoint, rt.roundTripperFactory, false, rt.http2Enabled)
	res, err := rt.timedRoundTrip(tr, request, logger)

	// decrement connection stats
	iter.PostRequest(endpoint)
	return res, err
}

func (rt *roundTripper) timedRoundTrip(tr http.RoundTripper, request *http.Request, logger logger.Logger) (*http.Response, error) {
	if rt.endpointTimeout <= 0 {
		return tr.RoundTrip(request)
	}

	reqCtx, cancel := context.WithTimeout(request.Context(), rt.endpointTimeout)
	request = request.WithContext(reqCtx)

	// unfortunately if the cancel function above is not called that
	// results in a vet error
	go func() {
		select {
		case <-reqCtx.Done():
			if reqCtx.Err() == context.DeadlineExceeded {
				logger.Error("backend-request-timeout", zap.Error(reqCtx.Err()), zap.String("vcap_request_id", request.Header.Get(handlers.VcapRequestIdHeader)))
			}
			cancel()
		}
	}()

	resp, err := tr.RoundTrip(request)
	if err != nil {
		cancel()
		return nil, err
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
	stickySessionCookieNames config.StringSet,
) {

	requestContainsStickySessionCookies := originalEndpointId != ""
	requestNotSentToRequestedApp := originalEndpointId != endpoint.PrivateInstanceId
	shouldSetVCAPID := requestContainsStickySessionCookies && requestNotSentToRequestedApp

	secure := false
	maxAge := 0
	sameSite := http.SameSite(0)
	expiry := time.Time{}

	for _, v := range response.Cookies() {
		if _, ok := stickySessionCookieNames[v.Name]; ok {
			shouldSetVCAPID = true

			if v.MaxAge < 0 {
				maxAge = v.MaxAge
			}
			secure = v.Secure
			sameSite = v.SameSite
			expiry = v.Expires
			break
		}
	}

	for _, v := range response.Cookies() {
		if v.Name == VcapCookieId {
			shouldSetVCAPID = false
			break
		}
	}

	if shouldSetVCAPID {
		// right now secure attribute would as equal to the JSESSION ID cookie (if present),
		// but override if set to true in config
		if secureCookies {
			secure = true
		}

		vcapIDCookie := &http.Cookie{
			Name:     VcapCookieId,
			Value:    endpoint.PrivateInstanceId,
			Path:     path,
			MaxAge:   maxAge,
			HttpOnly: true,
			Secure:   secure,
			SameSite: sameSite,
			Expires:  expiry,
		}

		if v := vcapIDCookie.String(); v != "" {
			response.Header.Add(CookieHeader, v)
		}
	}
}

func getStickySession(request *http.Request, stickySessionCookieNames config.StringSet) string {
	// Try choosing a backend using sticky session
	for stickyCookieName, _ := range stickySessionCookieNames {
		if _, err := request.Cookie(stickyCookieName); err == nil {
			if sticky, err := request.Cookie(VcapCookieId); err == nil {
				return sticky.Value
			}
		}
	}
	return ""
}

func requestSentToRouteService(request *http.Request) bool {
	sigHeader := request.Header.Get(routeservice.HeaderKeySignature)
	rsUrl := request.Header.Get(routeservice.HeaderKeyForwardedURL)
	return sigHeader != "" && rsUrl != ""
}

// Matches behavior of isReplayable() in standard library net/http/request.go
// https://github.com/golang/go/blob/5c489514bc5e61ad9b5b07bd7d8ec65d66a0512a/src/net/http/request.go
func isIdempotent(request *http.Request) bool {
	if request.Body == nil || request.Body == http.NoBody || request.GetBody != nil {
		switch request.Method {
		case "GET", "HEAD", "OPTIONS", "TRACE", "":
			return true
		}
		// The Idempotency-Key, while non-standard, is widely used to
		// mean a POST or other request is idempotent. See
		// https://golang.org/issue/19943#issuecomment-421092421
		if request.Header.Get("Idempotency-Key") != "" || request.Header.Get("X-Idempotency-Key") != "" {
			return true
		}
	}
	return false
}
