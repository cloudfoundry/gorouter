package routeservice

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/routeservice/header"
	"code.cloudfoundry.org/lager"
)

const (
	RouteServiceSignature    = "X-CF-Proxy-Signature"
	RouteServiceForwardedURL = "X-CF-Forwarded-Url"
	RouteServiceMetadata     = "X-CF-Proxy-Metadata"
)

var RouteServiceExpired = errors.New("Route service request expired")
var RouteServiceForwardedURLMismatch = errors.New("Route service forwarded url mismatch")

type RouteServiceConfig struct {
	routeServiceEnabled bool
	routeServiceTimeout time.Duration
	crypto              secure.Crypto
	cryptoPrev          secure.Crypto
	logger              lager.Logger
	recommendHttps      bool
}

type RouteServiceRequest struct {
	URLString      string
	ParsedUrl      *url.URL
	Signature      string
	Metadata       string
	ForwardedURL   string
	RecommendHttps bool
}

func NewRouteServiceConfig(
	logger lager.Logger,
	enabled bool,
	timeout time.Duration,
	crypto secure.Crypto,
	cryptoPrev secure.Crypto,
	recommendHttps bool,
) *RouteServiceConfig {
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

func (rs *RouteServiceConfig) Request(rsUrl, forwardedUrl string) (RouteServiceRequest, error) {
	var routeServiceArgs RouteServiceRequest
	sig, metadata, err := rs.generateSignatureAndMetadata(forwardedUrl)
	if err != nil {
		return routeServiceArgs, err
	}

	routeServiceArgs.URLString = rsUrl
	routeServiceArgs.Signature = sig
	routeServiceArgs.Metadata = metadata
	routeServiceArgs.ForwardedURL = forwardedUrl

	rsURL, err := url.Parse(rsUrl)
	if err != nil {
		return routeServiceArgs, err
	}
	routeServiceArgs.ParsedUrl = rsURL

	return routeServiceArgs, nil
}

func (rs *RouteServiceConfig) SetupRouteServiceRequest(request *http.Request, args RouteServiceRequest) {
}

func (rs *RouteServiceConfig) ValidateSignature(headers *http.Header, requestUrl string) error {
	metadataHeader := headers.Get(RouteServiceMetadata)
	signatureHeader := headers.Get(RouteServiceSignature)

	signature, err := header.SignatureFromHeaders(signatureHeader, metadataHeader, rs.crypto)
	if err != nil {
		rs.logger.Error("proxy.route-service.current_key", err)
		// Decrypt the head again trying to use the old key.
		if rs.cryptoPrev != nil {
			rs.logger.Error("proxy.route-service.current_key", err)
			signature, err = header.SignatureFromHeaders(signatureHeader, metadataHeader, rs.cryptoPrev)

			if err != nil {
				rs.logger.Error("proxy.route-service.previous_key", err)
			}
		}

		return err
	}

	err = rs.validateSignatureTimeout(signature)
	if err != nil {
		return err
	}

	return rs.validateForwardedURL(signature, requestUrl)
}

func (rs *RouteServiceConfig) generateSignatureAndMetadata(forwardedUrlRaw string) (string, string, error) {
	decodedURL, err := url.QueryUnescape(forwardedUrlRaw)
	if err != nil {
		rs.logger.Error("proxy.route-service.invalidForwardedURL", err)
		return "", "", err
	}
	signature := &header.Signature{
		RequestedTime: time.Now(),
		ForwardedUrl:  decodedURL,
	}

	signatureHeader, metadataHeader, err := header.BuildSignatureAndMetadata(rs.crypto, signature)
	if err != nil {
		return "", "", err
	}
	return signatureHeader, metadataHeader, nil
}

func (rs *RouteServiceConfig) validateSignatureTimeout(signature header.Signature) error {
	if time.Since(signature.RequestedTime) > rs.routeServiceTimeout {
		data := lager.Data{"forwarded-url": signature.ForwardedUrl, "requested-time": signature.RequestedTime}
		rs.logger.Error("proxy.route-service.timeout", RouteServiceExpired, data)
		return RouteServiceExpired
	}
	return nil
}

func (rs *RouteServiceConfig) validateForwardedURL(signature header.Signature, requestUrl string) error {
	var err error
	forwardedUrl := signature.ForwardedUrl
	requestUrl, err = url.QueryUnescape(requestUrl)
	if err != nil {
		rsErr := fmt.Errorf("%s: %s", RouteServiceForwardedURLMismatch, err)
		rs.logger.Error("proxy.route-service.forwarded-url.mismatch", rsErr)
		return err
	}

	if requestUrl != forwardedUrl {
		var err = RouteServiceForwardedURLMismatch
		rs.logger.Error("proxy.route-service.forwarded-url.mismatch", err, lager.Data{"request-url": requestUrl, "forwarded-url": forwardedUrl})
		return err
	}
	return nil
}
