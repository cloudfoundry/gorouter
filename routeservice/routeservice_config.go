package routeservice

import (
	"errors"
	"net/url"
	"time"

	"github.com/uber-go/zap"

	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/logger"
)

const (
	HeaderKeySignature    = "X-CF-Proxy-Signature"
	HeaderKeyForwardedURL = "X-CF-Forwarded-Url"
	HeaderKeyMetadata     = "X-CF-Proxy-Metadata"
)

var ErrExpired = errors.New("route service request expired")

type RouteServiceConfig struct {
	routeServiceEnabled              bool
	routeServiceHairpinning          bool
	routeServiceHairpinningAllowlist []string
	routeServiceTimeout              time.Duration
	crypto                           secure.Crypto
	cryptoPrev                       secure.Crypto
	logger                           logger.Logger
	recommendHttps                   bool
}

type RequestToSendToRouteService struct {
	URLString      string
	ParsedUrl      *url.URL
	Signature      string
	Metadata       string
	ForwardedURL   string
	RecommendHttps bool
}

type RequestReceivedFromRouteService struct {
	Metadata  string
	Signature string
	AppUrl    string
}

func NewRouteServiceConfig(
	logger logger.Logger,
	enabled bool,
	hairpinning bool,
	hairpinningallowlist []string,
	timeout time.Duration,
	crypto secure.Crypto,
	cryptoPrev secure.Crypto,
	recommendHttps bool,
) *RouteServiceConfig {
	return &RouteServiceConfig{
		routeServiceEnabled:              enabled,
		routeServiceTimeout:              timeout,
		routeServiceHairpinning:          hairpinning,
		routeServiceHairpinningAllowlist: hairpinningallowlist,
		crypto:                           crypto,
		cryptoPrev:                       cryptoPrev,
		logger:                           logger,
		recommendHttps:                   recommendHttps,
	}
}

func (rs *RouteServiceConfig) RouteServiceEnabled() bool {
	return rs.routeServiceEnabled
}

func (rs *RouteServiceConfig) RouteServiceRecommendHttps() bool {
	return rs.recommendHttps
}

func (rs *RouteServiceConfig) RouteServiceHairpinning() bool {
	return rs.routeServiceHairpinning
}

func (rs *RouteServiceConfig) RouteServiceHairpinningAllowlist() []string {
	return rs.routeServiceHairpinningAllowlist
}

func (rs *RouteServiceConfig) CreateRequest(rsUrl, forwardedUrl string) (RequestToSendToRouteService, error) {
	var routeServiceArgs RequestToSendToRouteService
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

func (rs *RouteServiceConfig) ValidateRequest(request RequestReceivedFromRouteService) (*SignatureContents, error) {

	signatureContents, err := SignatureContentsFromHeaders(request.Signature, request.Metadata, rs.crypto)
	if err != nil {
		if rs.cryptoPrev == nil {
			rs.logger.Error("proxy-route-service-current-key", zap.Error(err))
			return nil, err
		}

		rs.logger.Debug("proxy-route-service-current-key", zap.String("message", "Decrypt-only secret used to validate route service signature header"))
		// Decrypt the head again trying to use the old key.
		signatureContents, err = SignatureContentsFromHeaders(request.Signature, request.Metadata, rs.cryptoPrev)

		if err != nil {
			rs.logger.Error("proxy-route-service-previous-key", zap.Error(err))
			return nil, err
		}
	}

	err = rs.validateSignatureTimeout(signatureContents)
	if err != nil {
		return nil, err
	}

	return &signatureContents, nil
}

func (rs *RouteServiceConfig) generateSignatureAndMetadata(forwardedUrlRaw string) (string, string, error) {
	signatureContents := &SignatureContents{
		RequestedTime: time.Now(),
		ForwardedUrl:  forwardedUrlRaw,
	}

	signatureHeader, metadataHeader, err := BuildSignatureAndMetadata(rs.crypto, signatureContents)
	if err != nil {
		return "", "", err
	}
	return signatureHeader, metadataHeader, nil
}

func (rs *RouteServiceConfig) validateSignatureTimeout(signatureContents SignatureContents) error {
	if time.Since(signatureContents.RequestedTime) > rs.routeServiceTimeout {
		rs.logger.Error("proxy-route-service-timeout",
			zap.Error(ErrExpired),
			zap.String("forwarded-url", signatureContents.ForwardedUrl),
			zap.Time("request-time", signatureContents.RequestedTime),
		)
		return ErrExpired
	}
	return nil
}
