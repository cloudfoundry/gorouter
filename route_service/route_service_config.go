package route_service

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudfoundry/gorouter/common/secure"
	steno "github.com/cloudfoundry/gosteno"
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
	logger              *steno.Logger
}

type RouteServiceArgs struct {
	UrlString       string
	ParsedUrl       *url.URL
	Signature       string
	Metadata        string
	ForwardedUrlRaw string
}

func NewRouteServiceConfig(enabled bool, timeout time.Duration, crypto secure.Crypto, cryptoPrev secure.Crypto) *RouteServiceConfig {
	return &RouteServiceConfig{
		routeServiceEnabled: enabled,
		routeServiceTimeout: timeout,
		crypto:              crypto,
		cryptoPrev:          cryptoPrev,
		logger:              steno.NewLogger("router.proxy.route-service"),
	}
}

func (rs *RouteServiceConfig) RouteServiceEnabled() bool {
	return rs.routeServiceEnabled
}

func (rs *RouteServiceConfig) GenerateSignatureAndMetadata(forwardedUrlRaw string) (string, string, error) {
	signature := &Signature{
		RequestedTime: time.Now(),
		ForwardedUrl:  forwardedUrlRaw,
	}

	signatureHeader, metadataHeader, err := BuildSignatureAndMetadata(rs.crypto, signature)
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

	signature, err := SignatureFromHeaders(signatureHeader, metadataHeader, rs.crypto)
	if err != nil {
		rs.logger.Warnd(map[string]interface{}{"error": err.Error()}, "proxy.route-service.current_key")
		// Decrypt the head again trying to use the old key.
		if rs.cryptoPrev != nil {
			rs.logger.Warnd(map[string]interface{}{"error": err.Error()}, "proxy.route-service.current_key")
			signature, err = SignatureFromHeaders(signatureHeader, metadataHeader, rs.cryptoPrev)

			if err != nil {
				rs.logger.Warnd(map[string]interface{}{"error": err.Error()}, "proxy.route-service.previous_key")
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

func (rs *RouteServiceConfig) validateSignatureTimeout(signature Signature) error {
	if time.Since(signature.RequestedTime) > rs.routeServiceTimeout {
		rs.logger.Debug("proxy.route-service.timeout")
		return RouteServiceExpired
	}
	return nil
}

func (rs *RouteServiceConfig) validateForwardedUrl(signature Signature, requestUrl string) error {
	if requestUrl != signature.ForwardedUrl {
		var err = RouteServiceForwardedUrlMismatch
		rs.logger.Warnd(map[string]interface{}{"error": err.Error()}, "proxy.route-service.forwarded-url.mismatch")
		return err
	}
	return nil
}
