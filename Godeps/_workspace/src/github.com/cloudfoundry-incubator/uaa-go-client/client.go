package uaa_go_client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"

	trace "github.com/cloudfoundry-incubator/trace-logger"
	"github.com/dgrijalva/jwt-go"

	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"

	"github.com/cloudfoundry-incubator/uaa-go-client/config"
	"github.com/cloudfoundry-incubator/uaa-go-client/schema"
)

type uaaKey struct {
	Alg   string `json:"alg"`
	Value string `json:"value"`
}

type Client interface {
	FetchToken(forceUpdate bool) (*schema.Token, error)
	FetchKey() (string, error)
	DecodeToken(uaaToken string, desiredPermissions ...string) error
}

type UaaClient struct {
	clock            clock.Clock
	config           *config.Config
	client           *http.Client
	cachedToken      *schema.Token
	refetchTokenTime int64
	lock             *sync.Mutex
	logger           lager.Logger
	uaaPublicKey     string
	rwlock           sync.RWMutex
}

func NewClient(logger lager.Logger, cfg *config.Config, clock clock.Clock) (Client, error) {

	var (
		client *http.Client
		err    error
		uri    *url.URL
	)

	if cfg == nil {
		return nil, errors.New("Configuration cannot be nil")
	}

	uri, err = cfg.CheckEndpoint()
	if err != nil {
		return nil, err
	}

	if uri.Scheme == "https" {
		client, err = newSecureClient(cfg)
		if err != nil {
			return nil, err
		}
	} else {
		client = &http.Client{}
	}

	if cfg.ExpirationBufferInSec < 0 {
		cfg.ExpirationBufferInSec = config.DefaultExpirationBufferInSec
		logger.Info("Expiration buffer in seconds set to default", lager.Data{"value": config.DefaultExpirationBufferInSec})
	}

	return &UaaClient{
		logger: logger,
		config: cfg,
		client: client,
		clock:  clock,
		lock:   new(sync.Mutex),
	}, nil
}

func newSecureClient(cfg *config.Config) (*http.Client, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.SkipVerification},
	}

	client := &http.Client{Transport: tr}
	return client, nil
}

func (u *UaaClient) FetchToken(forceUpdate bool) (*schema.Token, error) {
	u.logger.Debug("fetching-token", lager.Data{"force-update": forceUpdate})

	if err := u.config.CheckCredentials(); err != nil {
		return nil, err
	}

	u.lock.Lock()
	defer u.lock.Unlock()

	if !forceUpdate && u.canReturnCachedToken() {
		u.logger.Debug("return-cached-token")
		return u.cachedToken, nil
	}

	retry := true
	var retryCount uint32 = 0
	var token *schema.Token
	var err error
	for retry == true {
		token, retry, err = u.doFetch()
		if token != nil {
			u.logger.Debug("successfully-fetched-token")
			break
		}
		if retry && retryCount < u.config.MaxNumberOfRetries {
			u.logger.Debug("retry-fetching-token", lager.Data{"retry-count": retryCount})
			retryCount++
			u.clock.Sleep(u.config.RetryInterval)
			continue
		} else {
			u.logger.Debug("failed-getting-token")
			return nil, err
		}
	}

	u.updateCachedToken(token)
	return token, nil
}

func (u *UaaClient) doFetch() (*schema.Token, bool, error) {
	values := url.Values{}
	values.Add("grant_type", "client_credentials")
	requestBody := values.Encode()
	tokenURL := fmt.Sprintf("%s/oauth/token", u.config.UaaEndpoint)
	request, err := http.NewRequest("POST", tokenURL, bytes.NewBuffer([]byte(requestBody)))
	if err != nil {
		return nil, false, err
	}

	request.SetBasicAuth(u.config.ClientName, u.config.ClientSecret)
	request.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	request.Header.Add("Accept", "application/json; charset=utf-8")
	trace.DumpRequest(request)
	u.logger.Debug("http-request", lager.Data{"endpoint": request.URL})

	resp, err := u.client.Do(request)
	if err != nil {
		u.logger.Debug("error-fetching-token", lager.Data{"error": err.Error()})
		return nil, true, err
	}
	defer resp.Body.Close()

	trace.DumpResponse(resp)
	u.logger.Debug("http-response", lager.Data{"status-code": resp.StatusCode})

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

	token := &schema.Token{}
	err = json.Unmarshal(body, token)
	if err != nil {
		u.logger.Debug("error-umarshalling-token", lager.Data{"error": err.Error()})
		return nil, false, err
	}
	return token, false, nil
}

func (u *UaaClient) FetchKey() (string, error) {
	logger := u.logger.Session("uaa-key-fetcher")
	logger.Info("fetch-key-started")
	defer logger.Info("fetch-key-completed")
	getKeyUrl := fmt.Sprintf("%s/token_key", u.config.UaaEndpoint)

	resp, err := u.client.Get(getKeyUrl)
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

	uaaKey := schema.UaaKey{}
	err = decoder.Decode(&uaaKey)
	if err != nil {
		logger.Error("error-in-unmarshaling-key", err)
		return "", err
	}
	logger.Info("fetch-key-successful")

	if err = checkPublicKey(uaaKey.Value); err != nil {
		logger.Error("error-not-valid-pem-key", err)
		return "", err
	}

	return uaaKey.Value, nil
}

func (u *UaaClient) DecodeToken(uaaToken string, desiredPermissions ...string) error {
	logger := u.logger.Session("decode-token")
	logger.Debug("start")
	defer logger.Debug("completed")
	var err error
	jwtToken, err := checkTokenFormat(uaaToken)
	if err != nil {
		return err
	}

	var token *jwt.Token
	var uaaKey string
	forceUaaKeyFetch := false

	for i := 0; i < 2; i++ {

		uaaKey, err = u.getUaaTokenKey(logger, forceUaaKeyFetch)

		if err == nil {
			token, err = jwt.Parse(jwtToken, func(t *jwt.Token) (interface{}, error) {
				return []byte(uaaKey), nil
			})

			if err != nil {
				if matchesError(err, jwt.ValidationErrorSignatureInvalid) {
					logger.Info("invalid-signature")
					forceUaaKeyFetch = true
					continue
				}
			}
		}

		break
	}

	if err != nil {
		return err
	}

	hasPermission := false
	permissions := token.Claims["scope"]

	a := permissions.([]interface{})

	for _, permission := range a {
		for _, desiredPermission := range desiredPermissions {
			if permission.(string) == desiredPermission {
				hasPermission = true
				break
			}
		}
	}

	if !hasPermission {
		err = errors.New("Token does not have '" + strings.Join(desiredPermissions, "', '") + "' scope")
		return err
	}

	return nil
}

func (u *UaaClient) canReturnCachedToken() bool {
	return u.cachedToken != nil && u.clock.Now().Unix() < u.refetchTokenTime
}

func (u *UaaClient) updateCachedToken(token *schema.Token) {
	u.logger.Debug("caching-token")
	u.cachedToken = token
	u.refetchTokenTime = u.clock.Now().Unix() + (token.ExpiresIn - u.config.ExpirationBufferInSec)
}

func checkPublicKey(key string) error {
	var block *pem.Block
	if block, _ = pem.Decode([]byte(key)); block == nil {
		return errors.New("Public uaa token must be PEM encoded")
	}
	return nil
}

func checkTokenFormat(token string) (string, error) {
	tokenParts := strings.Split(token, " ")
	if len(tokenParts) != 2 {
		return "", errors.New("Invalid token format")
	}

	tokenType, userToken := tokenParts[0], tokenParts[1]
	if !strings.EqualFold(tokenType, "bearer") {
		return "", errors.New("Invalid token type: " + tokenType)
	}

	return userToken, nil
}

func matchesError(err error, errorType uint32) bool {
	if validationError, ok := err.(*jwt.ValidationError); ok {
		return validationError.Errors&errorType == errorType
	}
	return false
}

func (u *UaaClient) getUaaTokenKey(logger lager.Logger, forceFetch bool) (string, error) {
	if u.getUaaPublicKey() == "" || forceFetch {
		logger.Debug("fetching-new-uaa-key")
		key, err := u.FetchKey()
		if err != nil {
			return key, err
		}
		err = checkPublicKey(key)
		if err != nil {
			return "", err
		}
		if u.getUaaPublicKey() == key {
			logger.Info("Fetched the same verification key from UAA")
		} else {
			logger.Info("Fetched a different verification key from UAA")
		}
		u.rwlock.Lock()
		defer u.rwlock.Unlock()
		u.uaaPublicKey = key
		return u.uaaPublicKey, nil
	}

	return u.getUaaPublicKey(), nil
}

func (u *UaaClient) getUaaPublicKey() string {
	u.rwlock.RLock()
	defer u.rwlock.RUnlock()
	return u.uaaPublicKey
}
