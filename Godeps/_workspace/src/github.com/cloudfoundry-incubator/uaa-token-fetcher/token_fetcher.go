package token_fetcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"time"

	trace "github.com/cloudfoundry-incubator/trace-logger"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

type OAuthConfig struct {
	TokenEndpoint string `yaml:"token_endpoint"`
	ClientName    string `yaml:"client_name"`
	ClientSecret  string `yaml:"client_secret"`
	Port          int    `yaml:"port"`
}

type TokenFetcherConfig struct {
	MaxNumberOfRetries   uint32
	RetryInterval        time.Duration
	ExpirationBufferTime int64
}

//go:generate counterfeiter -o fakes/fake_token_fetcher.go . TokenFetcher
type TokenFetcher interface {
	FetchToken(useCachedToken bool) (*Token, error)
}

type Token struct {
	AccessToken string `json:"access_token"`
	// Expire time in seconds
	ExpireTime int64 `json:"expires_in"`
}

type Fetcher struct {
	clock              clock.Clock
	config             *OAuthConfig
	client             *http.Client
	tokenFetcherConfig TokenFetcherConfig
	cachedToken        *Token
	refetchTokenTime   int64
	lock               *sync.Mutex
	logger             lager.Logger
}

func NewTokenFetcher(logger lager.Logger, config *OAuthConfig, tokenFetcherConfig TokenFetcherConfig, clock clock.Clock) (TokenFetcher, error) {
	if config == nil {
		return nil, errors.New("OAuth configuration cannot be nil")
	}

	if config.Port <= 0 || config.Port > 65535 {
		return nil, errors.New("OAuth port is not in valid range 1-65535")
	}

	if config.ClientName == "" {
		return nil, errors.New("OAuth Client ID cannot be empty")
	}

	if config.ClientSecret == "" {
		return nil, errors.New("OAuth Client Secret cannot be empty")
	}

	if config.TokenEndpoint == "" {
		return nil, errors.New("OAuth Token endpoint cannot be empty")
	}

	if tokenFetcherConfig.MaxNumberOfRetries == 0 {
		return nil, errors.New("Max number of retries cannot be zero")
	}

	if tokenFetcherConfig.ExpirationBufferTime < 0 {
		return nil, errors.New("Expiration buffer time cannot be negative")
	}

	return &Fetcher{
		logger:             logger,
		config:             config,
		client:             &http.Client{},
		tokenFetcherConfig: tokenFetcherConfig,
		clock:              clock,
		lock:               new(sync.Mutex),
	}, nil
}

func (f *Fetcher) FetchToken(useCachedToken bool) (*Token, error) {
	f.logger.Debug("fetching-token", lager.Data{"use-cached": useCachedToken})
	f.lock.Lock()
	defer f.lock.Unlock()

	if useCachedToken && f.canReturnCachedToken() {
		f.logger.Debug("return-cached-token")
		return f.cachedToken, nil
	}

	retry := true
	var retryCount uint32 = 1
	var token *Token
	var err error
	for retry == true {
		token, retry, err = f.doFetch()
		if token != nil {
			f.logger.Debug("successfully-fetched-token")
			break
		}
		if retry && retryCount < f.tokenFetcherConfig.MaxNumberOfRetries {
			f.logger.Debug("retry-fetching-token", lager.Data{"retry-count": retryCount})
			retryCount++
			f.clock.Sleep(f.tokenFetcherConfig.RetryInterval)
			continue
		} else {
			f.logger.Debug("failed-getting-token")
			return nil, err
		}
	}

	f.updateCachedToken(token)
	return token, nil
}

func (f *Fetcher) doFetch() (*Token, bool, error) {
	values := url.Values{}
	values.Add("grant_type", "client_credentials")
	requestBody := values.Encode()
	tokenURL := fmt.Sprintf("%s:%d/oauth/token", f.config.TokenEndpoint, f.config.Port)
	request, err := http.NewRequest("POST", tokenURL, bytes.NewBuffer([]byte(requestBody)))
	if err != nil {
		return nil, false, err
	}

	request.SetBasicAuth(f.config.ClientName, f.config.ClientSecret)
	request.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	request.Header.Add("Accept", "application/json; charset=utf-8")
	trace.DumpRequest(request)
	f.logger.Debug("http-request", lager.Data{"endpoint": request.URL})

	resp, err := f.client.Do(request)
	if err != nil {
		f.logger.Debug("error-fetching-token", lager.Data{"error": err.Error()})
		return nil, true, err
	}
	defer resp.Body.Close()

	trace.DumpResponse(resp)
	f.logger.Debug("http-response", lager.Data{"status-code": resp.StatusCode})

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}

	if resp.StatusCode != http.StatusOK {
		retry := false
		if resp.StatusCode >= http.StatusInternalServerError {
			retry = true
		}
		return nil, retry, errors.New(fmt.Sprintf("status code: %d, body: %s", resp.StatusCode, body))
	}

	token := &Token{}
	err = json.Unmarshal(body, token)
	if err != nil {
		f.logger.Debug("error-umarshalling-token", lager.Data{"error": err.Error()})
		return nil, false, err
	}
	return token, false, nil
}

func (f *Fetcher) canReturnCachedToken() bool {
	return f.cachedToken != nil && f.clock.Now().Unix() < f.refetchTokenTime
}

func (f *Fetcher) updateCachedToken(token *Token) {
	f.logger.Debug("caching-token")
	f.cachedToken = token
	f.refetchTokenTime = f.clock.Now().Unix() + (token.ExpireTime - f.tokenFetcherConfig.ExpirationBufferTime)
}
