package authentication

import (
	"encoding/json"
	"errors"
	"github.com/cloudfoundry-incubator/cf_http"
	"github.com/cloudfoundry-incubator/routing-api/metrics"
	"github.com/pivotal-golang/lager"
	"net/http"
)

//go:generate counterfeiter -o fakes/fake_uaa_key_fetcher.go . UaaKeyFetcher
type UaaKeyFetcher interface {
	FetchKey() (string, error)
}

type uaaKeyFetcher struct {
	uaaGetKeyEndpoint string
	httpClient        *http.Client
	logger            lager.Logger
}

type uaaKey struct {
	Alg   string `json:"alg"`
	Value string `json:"value"`
}

func NewUaaKeyFetcher(logger lager.Logger, uaaGetKeyEndpoint string) UaaKeyFetcher {
	return &uaaKeyFetcher{
		uaaGetKeyEndpoint: uaaGetKeyEndpoint,
		httpClient:        cf_http.NewClient(),
		logger:            logger,
	}
}

func (f *uaaKeyFetcher) FetchKey() (string, error) {
	logger := f.logger.Session("uaa-key-fetcher")
	logger.Info("fetch-key-started")
	defer logger.Info("fetch-key-completed")
	defer metrics.IncrementKeyVerificationRefreshCount()

	resp, err := f.httpClient.Get(f.uaaGetKeyEndpoint)
	if err != nil {
		logger.Error("error-in-fetching-key", err)
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		err = errors.New("http-error-fetching-key")
		logger.Error("http-error-fetching-key", err)
		return "", err
	}

	decoder := json.NewDecoder(resp.Body)

	uaaKey := uaaKey{}
	err = decoder.Decode(&uaaKey)
	if err != nil {
		logger.Error("error-in-unmarshaling-key", err)
		return "", err
	}
	logger.Info("fetch-key-successful")

	return uaaKey.Value, nil
}
