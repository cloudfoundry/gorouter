package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"code.cloudfoundry.org/gorouter/errorwriter"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/routeservice"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/route"
)

var hairpinningAllowlistPattern = regexp.MustCompile("(?i)^(\\*\\.)?[a-z\\d-]+(\\.[a-z\\d-]+)+$")

type RouteService struct {
	config                         *routeservice.RouteServiceConfig
	registry                       registry.Registry
	logger                         logger.Logger
	errorWriter                    errorwriter.ErrorWriter
	hairpinningAllowlistRegexCache []*regexp.Regexp
}

// NewRouteService creates a handler responsible for handling route services
func NewRouteService(
	config *routeservice.RouteServiceConfig,
	routeRegistry registry.Registry,
	logger logger.Logger,
	errorWriter errorwriter.ErrorWriter,
) negroni.Handler {
	allowlistRegex, err := createAllowlistRegex(config.RouteServiceHairpinningAllowlist())

	if err != nil {
		logger.Fatal("allowlist-entry-invalid", zap.Error(err))
	}
	return &RouteService{
		config:                         config,
		registry:                       routeRegistry,
		logger:                         logger,
		errorWriter:                    errorWriter,
		hairpinningAllowlistRegexCache: allowlistRegex,
	}
}

// createAllowlistRegex converts the DNS wildcard entry, e.g. *.example.com to the appropriate regular expression.
func createAllowlistRegex(allowlist []string) ([]*regexp.Regexp, error) {

	regexes := make([]*regexp.Regexp, 0, len(allowlist))

	for _, entry := range allowlist {
		wildcardPattern, err := WildcardDnsToRegex(entry)

		if err != nil {
			return nil, fmt.Errorf("invalid route service hairpinning allowlist entry: %s, %v", entry, err)
		}

		entryRegex := regexp.MustCompile(wildcardPattern)
		regexes = append(regexes, entryRegex)
	}
	return regexes, nil
}

func (r *RouteService) ServeHTTP(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
	reqInfo, err := ContextRequestInfo(req)
	if err != nil {
		r.logger.Fatal("request-info-err", zap.Error(err))
		return
	}
	if reqInfo.RoutePool == nil {
		r.logger.Fatal("request-info-err", zap.Error(errors.New("failed-to-access-RoutePool")))
		return
	}

	routeServiceURL := reqInfo.RoutePool.RouteServiceUrl()
	if routeServiceURL == "" {
		// No route service is associated with this request
		next(rw, req)
		return
	}

	if !r.config.RouteServiceEnabled() {
		r.logger.Info("route-service-unsupported")
		AddRouterErrorHeader(rw, "route_service_unsupported")
		r.errorWriter.WriteError(
			rw,
			http.StatusBadGateway,
			"Support for route services is disabled.",
			r.logger,
		)
		return
	}

	if IsWebSocketUpgrade(req) {
		r.logger.Info("route-service-unsupported")
		AddRouterErrorHeader(rw, "route_service_unsupported")
		r.errorWriter.WriteError(
			rw,
			http.StatusServiceUnavailable,
			"Websocket requests are not supported for routes bound to Route Services.",
			r.logger,
		)
		return
	}

	hasBeenToRouteService, err := r.ArrivedViaRouteService(req)
	if err != nil {
		r.logger.Error("signature-validation-failed", zap.Error(err))
		r.errorWriter.WriteError(
			rw,
			http.StatusBadRequest,
			"Failed to validate Route Service Signature",
			r.logger,
		)
		return
	}

	if hasBeenToRouteService {
		// Remove the headers since the backend should not see it
		req.Header.Del(routeservice.HeaderKeySignature)
		req.Header.Del(routeservice.HeaderKeyMetadata)
		req.Header.Del(routeservice.HeaderKeyForwardedURL)
		next(rw, req)
		return
	}

	// Update request with metadata for route service destination
	var recommendedScheme string
	if r.config.RouteServiceRecommendHttps() {
		recommendedScheme = "https"
	} else {
		recommendedScheme = "http"
	}
	forwardedURLRaw := recommendedScheme + "://" + hostWithoutPort(req.Host) + req.RequestURI
	routeServiceArgs, err := r.config.CreateRequest(routeServiceURL, forwardedURLRaw)
	if err != nil {
		r.logger.Error("route-service-failed", zap.Error(err))

		r.errorWriter.WriteError(
			rw,
			http.StatusInternalServerError,
			"Route service request failed.",
			r.logger,
		)
		return
	}

	hostWithoutPort := hostWithoutPort(routeServiceArgs.ParsedUrl.Host)
	escapedPath := routeServiceArgs.ParsedUrl.EscapedPath()
	if r.config.RouteServiceHairpinning() && r.AllowRouteServiceHairpinningRequest(route.Uri(hostWithoutPort+escapedPath)) {
		reqInfo.ShouldRouteToInternalRouteService = true
	}

	req.Header.Set(routeservice.HeaderKeySignature, routeServiceArgs.Signature)
	req.Header.Set(routeservice.HeaderKeyMetadata, routeServiceArgs.Metadata)
	req.Header.Set(routeservice.HeaderKeyForwardedURL, routeServiceArgs.ForwardedURL)
	reqInfo.RouteServiceURL = routeServiceArgs.ParsedUrl
	next(rw, req)
}

// AllowRouteServiceHairpinningRequest decides whether a route service request can be resolved
// internally via the route registry or should be handled as external request.
// If the provided route is not known to this gorouter's route registry, the request will always be external.
// If the route is known, the hairpinning allowlist is consulted (if defined).
//
// Only routes with host names that match an entry on the allowlist are resolved internally.
// Should the allowlist be empty, it is considered disabled and will allow internal resolution of any request
// that can be resolved via the gorouter's route registry.
//
// returns true to use internal resolution via this gorouter, false for external resolution via route service URL call.
func (r *RouteService) AllowRouteServiceHairpinningRequest(uri route.Uri) bool {

	route := r.registry.Lookup(uri)
	if route == nil {
		return false
	}

	if len(r.hairpinningAllowlistRegexCache) > 0 {
		host := route.Host()

		for _, entryRegex := range r.hairpinningAllowlistRegexCache {
			// check and compare allow list with DNS wildcard schema
			// "regex entry matches for the uri"
			if entryRegex.MatchString(host) {
				return true
			}
		}
		return false
	}
	return true
}

// WildcardDnsToRegex converts the given DNS name or wildcard to the corresponding regular expression.
func WildcardDnsToRegex(host string) (string, error) {
	if !hairpinningAllowlistPattern.MatchString(host) {
		return  "", fmt.Errorf("allowlist entry is not a DNS name or wildcard: %s", host)
	}

	var subdomainRegex string
	switch {
	case strings.HasPrefix(host, "*."):
		// DNS wildcard
		subdomainRegex = "^([^.]*)" + regexp.QuoteMeta(host[1:]) + "$"
	default:
		// DNS name
		subdomainRegex = "^" + regexp.QuoteMeta(host) + "$"
	}

	return subdomainRegex, nil
}

func (r *RouteService) IsRouteServiceTraffic(req *http.Request) bool {
	forwardedURLRaw := req.Header.Get(routeservice.HeaderKeyForwardedURL)
	signature := req.Header.Get(routeservice.HeaderKeySignature)

	if forwardedURLRaw == "" || signature == "" {
		return false
	}

	request := newRequestReceivedFromRouteService(forwardedURLRaw, req.Header)
	_, err := r.config.ValidateRequest(request)
	return err == nil
}

func (r *RouteService) ArrivedViaRouteService(req *http.Request) (bool, error) {
	reqInfo, err := ContextRequestInfo(req)
	if err != nil {
		r.logger.Fatal("request-info-err", zap.Error(err))
		return false, err
	}
	if reqInfo.RoutePool == nil {
		err = errors.New("failed-to-access-RoutePool")
		r.logger.Fatal("request-info-err", zap.Error(err))
		return false, err
	}

	var recommendedScheme string

	if r.config.RouteServiceRecommendHttps() {
		recommendedScheme = "https"
	} else {
		recommendedScheme = "http"
	}

	forwardedURLRaw := recommendedScheme + "://" + hostWithoutPort(req.Host) + req.RequestURI
	routeServiceURL := reqInfo.RoutePool.RouteServiceUrl()
	rsSignature := req.Header.Get(routeservice.HeaderKeySignature)

	if hasBeenToRouteService(routeServiceURL, rsSignature) {
		// A request from a route service destined for a backend instances
		request := newRequestReceivedFromRouteService(forwardedURLRaw, req.Header)
		validatedSig, err := r.config.ValidateRequest(request)
		if err != nil {
			return false, err
		}
		err = r.validateRouteServicePool(validatedSig, reqInfo.RoutePool)
		if err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func newRequestReceivedFromRouteService(appUrl string, requestHeaders http.Header) routeservice.RequestReceivedFromRouteService {
	return routeservice.RequestReceivedFromRouteService{
		AppUrl:    appUrl,
		Signature: requestHeaders.Get(routeservice.HeaderKeySignature),
		Metadata:  requestHeaders.Get(routeservice.HeaderKeyMetadata),
	}
}

func (r *RouteService) validateRouteServicePool(
	validatedSig *routeservice.SignatureContents,
	requestPool *route.EndpointPool,
) error {

	forwardedURL, err := url.ParseRequestURI(validatedSig.ForwardedUrl)
	if err != nil {
		return err
	}
	uri := route.Uri(hostWithoutPort(forwardedURL.Host) + forwardedURL.EscapedPath())
	forwardedPool := r.registry.Lookup(uri)
	if forwardedPool == nil {
		return fmt.Errorf(
			"original request URL %s does not exist in the routing table",
			uri.String(),
		)
	}

	if !route.PoolsMatch(requestPool, forwardedPool) {
		return fmt.Errorf(
			"route service forwarded URL %s%s does not match the original request URL %s",
			requestPool.Host(),
			requestPool.ContextPath(),
			uri.String(),
		)
	}
	return nil
}

func hasBeenToRouteService(rsUrl, sigHeader string) bool {
	return sigHeader != "" && rsUrl != ""
}
