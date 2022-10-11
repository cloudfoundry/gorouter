package route_fetcher

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/gorouter/config"
	goRouterLogger "code.cloudfoundry.org/gorouter/logger"
	uaa "github.com/cloudfoundry-community/go-uaa"
	"github.com/uber-go/zap"
	"golang.org/x/oauth2"
)

//go:generate counterfeiter -o fakes/fake_uaa_client.go . UAAClient
type UAAClient interface {
	Token(ctx context.Context) (*oauth2.Token, error)
}

type noOpUaaClient struct {
}

func (c *noOpUaaClient) Token(ctx context.Context) (*oauth2.Token, error) {
	return &oauth2.Token{}, nil
}

func NewUaaClient(logger goRouterLogger.Logger, clock clock.Clock, c *config.Config) UAAClient {
	if c.RoutingApi.AuthDisabled {
		logger.Info("using-noop-token-fetcher")
		return &noOpUaaClient{}
	}

	if c.OAuth.Port == -1 {
		logger.Fatal(
			"tls-not-enabled",
			zap.Error(errors.New("Gorouter requires TLS enabled to get OAuth token")),
			zap.String("token-endpoint", c.OAuth.TokenEndpoint),
			zap.Int("port", c.OAuth.Port),
		)
	}

	tokenUrl := fmt.Sprintf("https://%s:%d", c.OAuth.TokenEndpoint, c.OAuth.Port)
	tlsConfig := &tls.Config{InsecureSkipVerify: c.OAuth.SkipSSLValidation}
	if c.OAuth.CACerts != "" {
		certBytes, err := ioutil.ReadFile(c.OAuth.CACerts)
		if err != nil {
			logger.Fatal("failed-to-read-ca-cert-file", zap.Error(err))
		}

		caCertPool := x509.NewCertPool()
		if ok := caCertPool.AppendCertsFromPEM(certBytes); !ok {
			logger.Fatal("unable-to-load-ca-cert")
		}
		tlsConfig.RootCAs = caCertPool
	}

	tr := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	httpClient := &http.Client{Transport: tr}
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	uaaClient, err := uaa.New(tokenUrl, uaa.WithClientCredentials(c.OAuth.ClientName, c.OAuth.ClientSecret, uaa.JSONWebToken), uaa.WithClient(httpClient), uaa.WithSkipSSLValidation(c.OAuth.SkipSSLValidation))

	if err != nil {
		logger.Fatal("initialize-token-fetcher-error", zap.Error(err))
	}
	return uaaClient
}
