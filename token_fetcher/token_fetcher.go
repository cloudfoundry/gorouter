package token_fetcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/cloudfoundry/gorouter/config"
)

type TokenFetcher interface {
	FetchToken() (*Token, error)
}

type Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	ExpireTime  int    `json:"expires_in"`
}

type Fetcher struct {
	config *config.OAuthConfig
	client *http.Client
}

func NewTokenFetcher(config *config.OAuthConfig) *Fetcher {
	return &Fetcher{
		config: config,
		client: &http.Client{},
	}
}

func (f *Fetcher) FetchToken() (*Token, error) {
	values := url.Values{}
	values.Add("grant_type", "client_credentials")
	requestBody := values.Encode()

	request, _ := http.NewRequest("POST", f.config.TokenEndpoint, bytes.NewBuffer([]byte(requestBody)))
	request.SetBasicAuth(f.config.ClientName, f.config.ClientSecret)
	request.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	request.Header.Add("Accept", "application/json; charset=utf-8")

	resp, err := f.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(fmt.Sprintf("status code: %d, body: %s", resp.StatusCode, body))
	}

	token := &Token{}
	err = json.Unmarshal(body, token)
	if err != nil {
		return nil, err
	}

	return token, nil
}
