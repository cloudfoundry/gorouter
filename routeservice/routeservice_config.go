package routeservice

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/uber-go/zap"

	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/routeservice/header"
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
	logger              logger.Logger
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
	logger logger.Logger,
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

func (rs *RouteServiceConfig) RouteServiceRecommendHttps() bool {
	return rs.recommendHttps
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

func (rs *RouteServiceConfig) ValidateSignature(headers *http.Header, requestUrl string) error {
	metadataHeader := headers.Get(RouteServiceMetadata)
	signatureHeader := headers.Get(RouteServiceSignature)

	signature, err := header.SignatureFromHeaders(signatureHeader, metadataHeader, rs.crypto)
	if err != nil {
		if rs.cryptoPrev == nil {
			rs.logger.Error("proxy-route-service-current-key", zap.Error(err))
			return err
		}

		rs.logger.Debug("proxy-route-service-current-key", zap.String("message", "Decrypt-only secret used to validate route service signature header"))
		// Decrypt the head again trying to use the old key.
		signature, err = header.SignatureFromHeaders(signatureHeader, metadataHeader, rs.cryptoPrev)

		if err != nil {
			rs.logger.Error("proxy-route-service-previous-key", zap.Error(err))
			return err
		}
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
		rs.logger.Error("proxy-route-service-invalidForwardedURL", zap.Error(err))
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
		rs.logger.Error("proxy-route-service-timeout",
			zap.Error(RouteServiceExpired),
			zap.String("forwarded-url", signature.ForwardedUrl),
			zap.Time("request-time", signature.RequestedTime),
		)
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
		rs.logger.Error("proxy-route-service-forwarded-url-mismatch", zap.Error(rsErr))
		return err
	}

	if requestUrl != forwardedUrl {
		var err = RouteServiceForwardedURLMismatch
		rs.logger.Error("proxy-route-service-forwarded-url-mismatch", zap.Error(err),
			zap.String("request-url", requestUrl),
			zap.String("forwarded-url", forwardedUrl),
		)
		return err
	}
	return nil
}
