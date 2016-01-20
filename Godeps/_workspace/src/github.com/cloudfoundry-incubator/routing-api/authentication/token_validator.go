package authentication

import (
	"encoding/pem"
	"errors"
	"strings"
	"sync"

	"github.com/dgrijalva/jwt-go"
	"github.com/pivotal-golang/lager"
)

//go:generate counterfeiter -o fakes/fake_token_validator.go . TokenValidator
type TokenValidator interface {
	DecodeToken(userToken string, desiredPermissions ...string) error
	CheckPublicToken() error
}

type NullTokenValidator struct{}

func (_ NullTokenValidator) DecodeToken(_ string, _ ...string) error {
	return nil
}

func (_ NullTokenValidator) CheckPublicToken() error {
	return nil
}

type accessToken struct {
	logger        lager.Logger
	uaaPublicKey  string
	uaaKeyFetcher UaaKeyFetcher
	rwlock        sync.RWMutex
}

func NewAccessTokenValidator(logger lager.Logger, uaaPublicKey string, uaaKeyFetcher UaaKeyFetcher) TokenValidator {
	return &accessToken{
		logger:        logger,
		uaaPublicKey:  uaaPublicKey,
		uaaKeyFetcher: uaaKeyFetcher,
		rwlock:        sync.RWMutex{},
	}
}

func (accessToken *accessToken) DecodeToken(userToken string, desiredPermissions ...string) error {
	logger := accessToken.logger.Session("decode-token")
	logger.Debug("start")
	defer logger.Debug("completed")
	var err error
	jwtToken, err := checkTokenFormat(userToken)
	if err != nil {
		return err
	}

	var token *jwt.Token
	var uaaKey string
	forceUaaKeyFetch := false

	for i := 0; i < 2; i++ {
		uaaKey, err = accessToken.getUaaTokenKey(logger, forceUaaKeyFetch)

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

func (accessToken *accessToken) getUaaPublicKey() string {
	accessToken.rwlock.RLock()
	defer accessToken.rwlock.RUnlock()
	return accessToken.uaaPublicKey
}

func (accessToken *accessToken) CheckPublicToken() error {
	return checkPublicKey(accessToken.getUaaPublicKey())
}

func checkPublicKey(key string) error {
	var block *pem.Block
	if block, _ = pem.Decode([]byte(key)); block == nil {
		return errors.New("Public uaa token must be PEM encoded")
	}
	return nil
}

func (accessToken *accessToken) getUaaTokenKey(logger lager.Logger, forceFetch bool) (string, error) {
	if accessToken.getUaaPublicKey() == "" || forceFetch {
		logger.Debug("fetching-new-uaa-key")
		key, err := accessToken.uaaKeyFetcher.FetchKey()
		if err != nil {
			return key, err
		}
		err = checkPublicKey(key)
		if err != nil {
			return "", err
		}
		if accessToken.getUaaPublicKey() == key {
			logger.Info("Fetched the same verification key from UAA")
		} else {
			logger.Info("Fetched a different verification key from UAA")
		}
		accessToken.rwlock.Lock()
		defer accessToken.rwlock.Unlock()
		accessToken.uaaPublicKey = key
		return accessToken.uaaPublicKey, nil
	}

	return accessToken.getUaaPublicKey(), nil
}

func checkTokenFormat(token string) (string, error) {
	tokenParts := strings.Split(token, " ")
	if len(tokenParts) != 2 {
		return "", errors.New("Invalid token format")
	}

	tokenType, userToken := tokenParts[0], tokenParts[1]
	if tokenType != "bearer" {
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
