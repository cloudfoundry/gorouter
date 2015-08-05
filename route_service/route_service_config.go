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

var routeServiceExpired = errors.New("Route service request expired")

type RouteServiceConfig struct {
	routeServiceEnabled bool
	routeServiceTimeout time.Duration
	crypto              secure.Crypto
	cryptoPrev          secure.Crypto
	logger              *steno.Logger
}

type RouteServiceArgs struct {
	UrlString string
	ParsedUrl *url.URL
	Signature string
	Metadata  string
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

func (rs *RouteServiceConfig) GenerateSignatureAndMetadata() (string, string, error) {
	signatureHeader, metadataHeader, err := BuildSignatureAndMetadata(rs.crypto)
	if err != nil {
		return "", "", err
	}
	return signatureHeader, metadataHeader, nil
}

func (rs *RouteServiceConfig) SetupRouteServiceRequest(request *http.Request, args RouteServiceArgs) {
	rs.logger.Debug("proxy.route-service")
	request.Header.Set(RouteServiceSignature, args.Signature)
	request.Header.Set(RouteServiceMetadata, args.Metadata)

	clientRequestUrl := request.URL.Scheme + "://" + request.URL.Host + request.URL.Opaque

	request.Header.Set(RouteServiceForwardedUrl, clientRequestUrl)

	request.Host = args.ParsedUrl.Host
	request.URL = args.ParsedUrl
}

func (rs *RouteServiceConfig) ValidateSignature(headers *http.Header) error {
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
	}

	if err != nil {
		return err
	}

	if time.Since(signature.RequestedTime) > rs.routeServiceTimeout {
		rs.logger.Debug("proxy.route-service.timeout")
		return routeServiceExpired
	}

	return nil
}
