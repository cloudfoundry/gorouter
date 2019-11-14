package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/routeservice"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"

	"code.cloudfoundry.org/gorouter/route"
)

type RouteService struct {
	config   *routeservice.RouteServiceConfig
	registry registry.Registry
	logger   logger.Logger
}

// NewRouteService creates a handler responsible for handling route services
func NewRouteService(config *routeservice.RouteServiceConfig, routeRegistry registry.Registry, logger logger.Logger) negroni.Handler {
	return &RouteService{
		config:   config,
		registry: routeRegistry,
		logger:   logger,
	}
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
		next(rw, req)
		return
	}
	// Attempted to use a route service when it is not supported
	if !r.config.RouteServiceEnabled() {
		r.logger.Info("route-service-unsupported")

		AddRouterErrorHeader(rw, "route_service_unsupported")

		writeStatus(
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

		writeStatus(
			rw,
			http.StatusServiceUnavailable,
			"Websocket requests are not supported for routes bound to Route Services.",
			r.logger,
		)
		return
	}

	var recommendedScheme string

	if r.config.RouteServiceRecommendHttps() {
		recommendedScheme = "https"
	} else {
		recommendedScheme = "http"
	}

	forwardedURLRaw := recommendedScheme + "://" + hostWithoutPort(req.Host) + req.RequestURI
	hasBeenToRouteService, err := r.ArrivedViaRouteService(req)
	if err != nil {
		r.logger.Error("signature-validation-failed", zap.Error(err))
		writeStatus(
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
	} else {
		var err error
		routeServiceArgs, err := r.config.Request(routeServiceURL, forwardedURLRaw)
		if err != nil {
			r.logger.Error("route-service-failed", zap.Error(err))

			writeStatus(
				rw,
				http.StatusInternalServerError,
				"Route service request failed.",
				r.logger,
			)
			return
		}
		req.Header.Set(routeservice.HeaderKeySignature, routeServiceArgs.Signature)
		req.Header.Set(routeservice.HeaderKeyMetadata, routeServiceArgs.Metadata)
		req.Header.Set(routeservice.HeaderKeyForwardedURL, routeServiceArgs.ForwardedURL)

		reqInfo.RouteServiceURL = routeServiceArgs.ParsedUrl

		rsu := routeServiceArgs.ParsedUrl
		uri := route.Uri(hostWithoutPort(rsu.Host) + rsu.EscapedPath())
		if r.config.RouteServiceHairpinning() && r.registry.Lookup(uri) != nil {
			reqInfo.ShouldRouteToInternalRouteService = true
		}
	}

	next(rw, req)
}

func (r *RouteService) IsRouteServiceTraffic(req *http.Request) bool {
	forwardedURLRaw := req.Header.Get(routeservice.HeaderKeyForwardedURL)
	signature := req.Header.Get(routeservice.HeaderKeySignature)

	if forwardedURLRaw == "" || signature == "" {
		return false
	}
	_, err := r.config.ValidatedSignature(&req.Header, forwardedURLRaw)
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
		validatedSig, err := r.config.ValidatedSignature(&req.Header, forwardedURLRaw)
		if err != nil {
			return false, err
		}
		err = r.validateRouteServicePool(validatedSig, reqInfo)
		if err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (r *RouteService) validateRouteServicePool(validatedSig *routeservice.Signature, reqInfo *RequestInfo) error {
	forwardedURL, err := url.ParseRequestURI(validatedSig.ForwardedUrl)
	if err != nil {
		return err
	}
	uri := route.Uri(hostWithoutPort(forwardedURL.Host) + forwardedURL.EscapedPath())
	forwardedPool := r.registry.Lookup(uri)
	if forwardedPool == nil {
		return fmt.Errorf("original request URL %s does not exist in the routing table", uri.String())
	}
	reqPool := reqInfo.RoutePool
	if !route.PoolsMatch(reqPool, forwardedPool) {
		return fmt.Errorf("route service forwarded URL %s%s does not match the original request URL %s", reqPool.Host(), reqPool.ContextPath(), uri.String())
	}
	return nil
}

func hasBeenToRouteService(rsUrl, sigHeader string) bool {
	return sigHeader != "" && rsUrl != ""
}
