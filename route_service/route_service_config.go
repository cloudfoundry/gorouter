package route_service

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/route_service/header"
	"code.cloudfoundry.org/lager"
)

const (
	RouteServiceSignature    = "X-CF-Proxy-Signature"
	RouteServiceForwardedUrl = "X-CF-Forwarded-Url"
	RouteServiceMetadata     = "X-CF-Proxy-Metadata"
)

var RouteServiceExpired = errors.New("Route service request expired")
var RouteServiceForwardedUrlMismatch = errors.New("Route service forwarded url mismatch")

type RouteServiceConfig struct {
	routeServiceEnabled bool
	routeServiceTimeout time.Duration
	crypto              secure.Crypto
	cryptoPrev          secure.Crypto
	logger              lager.Logger
	recommendHttps      bool
}

type RouteServiceArgs struct {
	UrlString       string
	ParsedUrl       *url.URL
	Signature       string
	Metadata        string
	ForwardedUrlRaw string
	RecommendHttps  bool
}

func NewRouteServiceConfig(logger lager.Logger, enabled bool, timeout time.Duration, crypto secure.Crypto, cryptoPrev secure.Crypto, recommendHttps bool) *RouteServiceConfig {
	return &RouteServiceConfig{
		routeServiceEnabled: enabled,
		routeServiceTimeout: timeout,
		crypto:              crypto,
		cryptoPrev:          cryptoPrev,
		logger:              logger,
		recommendHttps:      recommendHttps,
	}
}

func (rs *RouteServiceConfig) RouteServiceEnabled() bool {
	return rs.routeServiceEnabled
}

func (rs *RouteServiceConfig) GenerateSignatureAndMetadata(forwardedUrlRaw string) (string, string, error) {
	signature := &header.Signature{
		RequestedTime: time.Now(),
		ForwardedUrl:  forwardedUrlRaw,
	}

	signatureHeader, metadataHeader, err := header.BuildSignatureAndMetadata(rs.crypto, signature)
	if err != nil {
		return "", "", err
	}
	return signatureHeader, metadataHeader, nil
}

func (rs *RouteServiceConfig) SetupRouteServiceRequest(request *http.Request, args RouteServiceArgs) {
	rs.logger.Debug("proxy.route-service")
	request.Header.Set(RouteServiceSignature, args.Signature)
	request.Header.Set(RouteServiceMetadata, args.Metadata)
	request.Header.Set(RouteServiceForwardedUrl, args.ForwardedUrlRaw)

	request.Host = args.ParsedUrl.Host
	request.URL = args.ParsedUrl
}

func (rs *RouteServiceConfig) ValidateSignature(headers *http.Header, requestUrl string) error {
	metadataHeader := headers.Get(RouteServiceMetadata)
	signatureHeader := headers.Get(RouteServiceSignature)

	signature, err := header.SignatureFromHeaders(signatureHeader, metadataHeader, rs.crypto)
	if err != nil {
		rs.logger.Info("proxy.route-service.current_key", lager.Data{"error": err.Error()})
		// Decrypt the head again trying to use the old key.
		if rs.cryptoPrev != nil {
			rs.logger.Info("proxy.route-service.current_key", lager.Data{"error": err.Error()})
			signature, err = header.SignatureFromHeaders(signatureHeader, metadataHeader, rs.cryptoPrev)

			if err != nil {
				rs.logger.Info("proxy.route-service.previous_key", lager.Data{"error": err.Error()})
			}
		}

		return err
	}

	err = rs.validateSignatureTimeout(signature)
	if err != nil {
		return err
	}

	return rs.validateForwardedUrl(signature, requestUrl)
}

func (rs *RouteServiceConfig) validateSignatureTimeout(signature header.Signature) error {
	if time.Since(signature.RequestedTime) > rs.routeServiceTimeout {
		rs.logger.Debug("proxy.route-service.timeout")
		return RouteServiceExpired
	}
	return nil
}

func (rs *RouteServiceConfig) validateForwardedUrl(signature header.Signature, requestUrl string) error {
	if requestUrl != signature.ForwardedUrl {
		var err = RouteServiceForwardedUrlMismatch
		rs.logger.Info("proxy.route-service.forwarded-url.mismatch", lager.Data{"error": err.Error()})
		return err
	}
	return nil
}
