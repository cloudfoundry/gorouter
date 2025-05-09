package round_tripper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	router_http "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
	"code.cloudfoundry.org/gorouter/proxy/fails"
	"code.cloudfoundry.org/gorouter/proxy/utils"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/routeservice"
)

const (
	VcapCookieId                             = "__VCAP_ID__"
	CookieHeader                             = "Set-Cookie"
	BadGatewayMessage                        = "502 Bad Gateway: Registered endpoint failed to handle the request."
	HostnameErrorMessage                     = "503 Service Unavailable"
	InvalidCertificateMessage                = "526 Invalid SSL Certificate"
	SSLHandshakeMessage                      = "525 SSL Handshake Failed"
	SSLCertRequiredMessage                   = "496 SSL Certificate Required"
	ContextCancelledMessage                  = "499 Request Cancelled"
	HTTP2Protocol                            = "http2"
	AuthNegotiateHeaderCookieMaxAgeInSeconds = 60
)

var (
	NoEndpointsAvailable   = errors.New("No endpoints available")
	TooManyResponseHeaders = errors.New("Too many response headers")
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
	logger *slog.Logger,
	combinedReporter metrics.MetricReporter,
	errHandler errorHandler,
	routeServicesTransport http.RoundTripper,
	cfg *config.Config,
) ProxyRoundTripper {

	return &roundTripper{
		logger:                 logger,
		combinedReporter:       combinedReporter,
		roundTripperFactory:    roundTripperFactory,
		retriableClassifier:    retriableClassifiers,
		errorHandler:           errHandler,
		routeServicesTransport: routeServicesTransport,
		config:                 cfg,
	}
}

type roundTripper struct {
	logger                 *slog.Logger
	combinedReporter       metrics.MetricReporter
	roundTripperFactory    RoundTripperFactory
	retriableClassifier    fails.Classifier
	errorHandler           errorHandler
	routeServicesTransport http.RoundTripper
	config                 *config.Config
}

func (rt *roundTripper) RoundTrip(originalRequest *http.Request) (*http.Response, error) {
	var err error
	var res *http.Response
	var endpoint *route.Endpoint

	request := originalRequest.Clone(originalRequest.Context())
	request, trace := traceRequest(request)

	if request.Body != nil {
		// Temporarily disable closing of the body while in the RoundTrip function, since
		// the underlying Transport will close the client request body.
		// https://github.com/golang/go/blob/ab5d9f5831cd267e0d8e8954cfe9987b737aec9c/src/net/http/request.go#L179-L182

		request.Body = io.NopCloser(request.Body)
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

	stickyEndpointID, mustBeSticky := handlers.GetStickySession(request, rt.config.StickySessionCookieNames, rt.config.StickySessionsForAuthNegotiate)
	numberOfEndpoints := reqInfo.RoutePool.NumEndpoints()
	iter := reqInfo.RoutePool.Endpoints(rt.logger, stickyEndpointID, mustBeSticky, rt.config.LoadBalanceAZPreference, rt.config.Zone)

	// The selectEndpointErr needs to be tracked separately. If we get an error
	// while selecting an endpoint we might just have run out of routes. In
	// such cases the last error that was returned by the round trip should be
	// used to produce a 502 instead of the error returned from selecting the
	// endpoint which would result in a 404 Not Found.
	var selectEndpointErr error
	var maxAttempts int
	if reqInfo.RouteServiceURL == nil {
		maxAttempts = max(rt.config.Backends.MaxAttempts, 1)
	} else {
		maxAttempts = rt.config.RouteServiceConfig.MaxAttempts
	}
	triedEndpoints := map[string]bool{}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		logger := rt.logger

		// Reset the trace to prepare for new times and prevent old data from polluting our results.
		trace.Reset()

		if reqInfo.RouteServiceURL == nil {
			// Because this for-loop is 1-indexed, we substract one from the attempt value passed to selectEndpoint,
			// which expects a 0-indexed value
			endpoint, selectEndpointErr = rt.selectEndpoint(iter, attempt-1)

			if attempt > 1 {
				if attempt > reqInfo.RoutePool.NumEndpoints() {
					// check if new endpoints were registered
					if selectEndpointErr == nil {
						if _, found := triedEndpoints[endpoint.CanonicalAddr()]; found {
							break
						}
					}
				}
			}

			if selectEndpointErr != nil {
				logger.Error("select-endpoint-failed", slog.String("host", reqInfo.RoutePool.Host()), log.ErrAttr(selectEndpointErr))
				break
			}
			logger = logger.With(slog.Group("route-endpoint", endpoint.ToLogData()...))
			triedEndpoints[endpoint.CanonicalAddr()] = true
			reqInfo.RouteEndpoint = endpoint

			logger.Debug("backend", slog.Int("attempt", attempt))
			if endpoint.IsTLS() {
				request.URL.Scheme = "https"
			} else {
				request.URL.Scheme = "http"
			}
			res, err = rt.backendRoundTrip(request, endpoint, iter, logger)

			logger = logger.With(
				slog.Int("attempt", attempt),
				slog.String("vcap_request_id", request.Header.Get(handlers.VcapRequestIdHeader)),
				slog.Int("num-endpoints", numberOfEndpoints),
				slog.Bool("got-connection", trace.GotConn()),
				slog.Bool("wrote-headers", trace.WroteHeaders()),
				slog.Bool("conn-reused", trace.ConnReused()),
				slog.Float64("dns-lookup-time", trace.DnsTime()),
				slog.Float64("dial-time", trace.DialTime()),
				slog.Float64("tls-handshake-time", trace.TlsTime()),
				slog.String("local-address", trace.LocalAddr()),
			)

			if err != nil {
				reqInfo.FailedAttempts++
				reqInfo.LastFailedAttemptFinishedAt = time.Now()
				retriable, err := rt.isRetriable(request, err, trace)

				logger.Error("backend-endpoint-failed",
					log.ErrAttr(err),
					slog.Bool("retriable", retriable),
				)

				iter.EndpointFailed(err)

				if retriable {
					continue
				}
			}

			if res != nil && err == nil {
				err = checkResponseHeaders(rt.config.MaxResponseHeaders, res.Header)
				if err != nil {
					logger.Error("backend-too-many-response-headers",
						log.ErrAttr(err),
						slog.Bool("retriable", false),
					)
					break
				}
			}

			break
		} else {
			logger.Debug(
				"route-service",
				slog.Any("route-service-url", log.StructValue(reqInfo.RouteServiceURL)),
				slog.Int("attempt", attempt),
			)

			endpoint = &route.Endpoint{
				Tags: map[string]string{},
			}
			reqInfo.RouteEndpoint = endpoint
			request.Host = reqInfo.RouteServiceURL.Host
			request.URL = new(url.URL)
			*request.URL = *reqInfo.RouteServiceURL

			var roundTripper http.RoundTripper
			roundTripper = GetRoundTripper(endpoint, rt.roundTripperFactory, true, rt.config.EnableHTTP2)
			if reqInfo.ShouldRouteToInternalRouteService {
				roundTripper = rt.routeServicesTransport
			}

			res, err = rt.timedRoundTrip(roundTripper, request, logger)

			logger = logger.With(
				slog.Int("attempt", attempt),
				slog.String("vcap_request_id", request.Header.Get(handlers.VcapRequestIdHeader)),
				slog.Int("num-endpoints", numberOfEndpoints),
				slog.Bool("got-connection", trace.GotConn()),
				slog.Bool("wrote-headers", trace.WroteHeaders()),
				slog.Bool("conn-reused", trace.ConnReused()),
				slog.Float64("dns-lookup-time", trace.DnsTime()),
				slog.Float64("dial-time", trace.DialTime()),
				slog.Float64("tls-handshake-time", trace.TlsTime()),
				slog.String("local-address", trace.LocalAddr()),
			)

			if err != nil {
				reqInfo.FailedAttempts++
				reqInfo.LastFailedAttemptFinishedAt = time.Now()
				retriable, err := rt.isRetriable(request, err, trace)

				logger.Error(
					"route-service-connection-failed",
					slog.String("route-service-endpoint", request.URL.String()),
					log.ErrAttr(err),
					slog.Bool("retriable", retriable),
				)

				if retriable {
					continue
				}
			}

			if res != nil && err == nil {
				err = checkResponseHeaders(rt.config.MaxResponseHeaders, res.Header)
				if err != nil {
					logger.Error("route-service-too-many-response-headers",
						log.ErrAttr(err),
						slog.Bool("retriable", false),
					)
					break
				}

			}

			if res != nil && (res.StatusCode < 200 || res.StatusCode >= 300) {
				logger.Info(
					"route-service-response",
					slog.String("route-service-endpoint", request.URL.String()),
					slog.Int("status-code", res.StatusCode),
				)
			}

			break
		}
	}

	// if the client disconnects before response is sent then return context.Canceled (499) instead of the gateway error
	if err != nil && errors.Is(originalRequest.Context().Err(), context.Canceled) && !errors.Is(err, context.Canceled) {
		rt.logger.Error("gateway-error-and-original-request-context-cancelled", log.ErrAttr(err))
		err = originalRequest.Context().Err()
		if originalRequest.Body != nil {
			_ = originalRequest.Body.Close()
		}
	}

	// If we have an error from the round trip, we prefer it over errors
	// returned from selecting the endpoint, see declaration of
	// selectEndpointErr for details.
	if err == nil {
		err = selectEndpointErr
	}

	if err != nil {
		rt.errorHandler.HandleError(reqInfo.ProxyResponseWriter, err)
		if handlers.IsWebSocketUpgrade(request) {
			rt.combinedReporter.CaptureWebSocketFailure()
		}
		return nil, err
	}

	// Round trip was successful at this point
	reqInfo.RoundTripSuccessful = true

	// Set status code for access log
	if res != nil {
		reqInfo.ProxyResponseWriter.SetStatus(res.StatusCode)
	}

	// Write metric for ws upgrades
	if handlers.IsWebSocketUpgrade(request) {
		rt.combinedReporter.CaptureWebSocketUpdate()
	}

	// Record the times from the last attempt, but only if it succeeded.
	reqInfo.DnsStartedAt = trace.DnsStart()
	reqInfo.DnsFinishedAt = trace.DnsDone()
	reqInfo.DialStartedAt = trace.DialStart()
	reqInfo.DialFinishedAt = trace.DialDone()
	reqInfo.TlsHandshakeStartedAt = trace.TlsStart()
	reqInfo.TlsHandshakeFinishedAt = trace.TlsDone()
	reqInfo.LocalAddress = trace.LocalAddr()

	if res != nil && endpoint.PrivateInstanceId != "" && !requestSentToRouteService(request) {
		setupStickySession(
			res, endpoint, stickyEndpointID, rt.config.SecureCookies,
			reqInfo.RoutePool.ContextPath(), rt.config.StickySessionCookieNames,
			rt.config.StickySessionsForAuthNegotiate,
		)
	}

	return res, nil
}

func (rt *roundTripper) CancelRequest(request *http.Request) {
	endpoint, err := handlers.GetEndpoint(request.Context())
	if err != nil {
		return
	}

	tr := GetRoundTripper(endpoint, rt.roundTripperFactory, false, rt.config.EnableHTTP2)
	tr.CancelRequest(request)
}

func (rt *roundTripper) backendRoundTrip(request *http.Request, endpoint *route.Endpoint, iter route.EndpointIterator, logger *slog.Logger) (*http.Response, error) {
	request.URL.Host = endpoint.CanonicalAddr()
	request.Header.Set("X-CF-ApplicationID", endpoint.ApplicationId)
	request.Header.Set("X-CF-InstanceIndex", endpoint.PrivateInstanceIndex)
	setRequestXCfInstanceId(request, endpoint)

	// increment connection stats
	iter.PreRequest(endpoint)

	rt.combinedReporter.CaptureRoutingRequest(endpoint)
	tr := GetRoundTripper(endpoint, rt.roundTripperFactory, false, rt.config.EnableHTTP2)
	res, err := rt.timedRoundTrip(tr, request, logger)

	// decrement connection stats
	iter.PostRequest(endpoint)
	return res, err
}

func (rt *roundTripper) timedRoundTrip(tr http.RoundTripper, request *http.Request, logger *slog.Logger) (*http.Response, error) {
	if rt.config.EndpointTimeout <= 0 || handlers.IsWebSocketUpgrade(request) {
		return tr.RoundTrip(request)
	}

	reqCtx, cancel := context.WithTimeout(request.Context(), rt.config.EndpointTimeout)
	request = request.WithContext(reqCtx)

	// unfortunately if the cancel function above is not called that
	// results in a vet error
	vrid := request.Header.Get(handlers.VcapRequestIdHeader)
	go func() {
		<-reqCtx.Done()
		if errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			logger.Error("backend-request-timeout", log.ErrAttr(reqCtx.Err()), slog.String("vcap_request_id", vrid))
		}
		cancel()
	}()

	resp, err := tr.RoundTrip(request)
	if err != nil {
		cancel()
		return nil, err
	}

	return resp, err
}

func (rt *roundTripper) selectEndpoint(iter route.EndpointIterator, attempt int) (*route.Endpoint, error) {
	endpoint := iter.Next(attempt)
	if endpoint == nil {
		return nil, NoEndpointsAvailable
	}

	return endpoint, nil
}

func checkResponseHeaders(maxCount int, headers http.Header) error {
	if maxCount > 0 {
		// Go doesn't split header values on commas, instead it only splits the value when it's
		// provided via repeated header keys. We can therefore get the number of header lines by
		// checking how many values are in the map.
		hdrCount := 0
		for _, vv := range headers {
			hdrCount += len(vv)
		}

		if hdrCount > maxCount {
			return TooManyResponseHeaders
		}
	}

	return nil
}

func setRequestXCfInstanceId(request *http.Request, endpoint *route.Endpoint) {
	value := endpoint.PrivateInstanceId
	if value == "" {
		value = endpoint.CanonicalAddr()
	}

	request.Header.Set(router_http.CfInstanceIdHeader, value)
}

func setupStickySession(
	response *http.Response,
	endpoint *route.Endpoint,
	originalEndpointId string,
	secureCookies bool,
	path string,
	stickySessionCookieNames config.StringSet,
	authNegotiateSticky bool,
) {

	requestContainsStickySessionCookies := originalEndpointId != ""
	requestNotSentToRequestedApp := originalEndpointId != endpoint.PrivateInstanceId
	responseContainsAuthNegotiateHeader := strings.HasPrefix(strings.ToLower(response.Header.Get("WWW-Authenticate")), "negotiate")
	shouldSetVCAPID := ((authNegotiateSticky && responseContainsAuthNegotiateHeader) || requestContainsStickySessionCookies) && requestNotSentToRequestedApp

	secure := false
	maxAge := 0
	sameSite := http.SameSite(0)
	expiry := time.Time{}
	partitioned := false

	if responseContainsAuthNegotiateHeader && authNegotiateSticky {
		maxAge = AuthNegotiateHeaderCookieMaxAgeInSeconds
		sameSite = http.SameSiteStrictMode
	} else {
		for _, v := range response.Cookies() {
			if _, ok := stickySessionCookieNames[v.Name]; ok {
				shouldSetVCAPID = true

				if v.MaxAge < 0 {
					maxAge = v.MaxAge
				}
				secure = v.Secure
				sameSite = v.SameSite
				expiry = v.Expires
				partitioned = v.Partitioned

				break
			}
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

		vcapIDCookie := http.Cookie{
			Name:        VcapCookieId,
			Value:       endpoint.PrivateInstanceId,
			Path:        path,
			MaxAge:      maxAge,
			HttpOnly:    true,
			Secure:      secure,
			SameSite:    sameSite,
			Expires:     expiry,
			Partitioned: partitioned,
		}

		if v := vcapIDCookie.String(); v != "" {
			response.Header.Add(CookieHeader, v)
		}
	}
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

func (rt *roundTripper) isRetriable(request *http.Request, err error, trace *requestTracer) (bool, error) {
	// if the context has been cancelled we do not perform further retries
	if request.Context().Err() != nil {
		return false, fmt.Errorf("%w (%w)", request.Context().Err(), err)
	}

	// io.EOF errors are considered safe to retry for certain requests
	// Replace the error here to track this state when classifying later.
	if err == io.EOF && isIdempotent(request) {
		err = fails.IdempotentRequestEOFError
	}
	// We can retry for sure if we never obtained a connection
	// since there is no way any data was transmitted. If headers could not
	// be written in full, the request should also be safe to retry.
	if !trace.GotConn() || !trace.WroteHeaders() {
		err = fmt.Errorf("%w (%w)", fails.IncompleteRequestError, err)
	}

	retriable := rt.retriableClassifier.Classify(err)
	return retriable, err
}
