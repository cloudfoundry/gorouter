package authentication

import (
	"encoding/pem"
	"errors"
	"strings"

	"github.com/dgrijalva/jwt-go"
)

//go:generate counterfeiter -o fakes/fake_token.go . Token
type Token interface {
	DecodeToken(userToken string, desiredPermissions ...string) error
	CheckPublicToken() error
}

type NullToken struct{}

func (_ NullToken) DecodeToken(_ string, _ ...string) error {
	return nil
}

func (_ NullToken) CheckPublicToken() error {
	return nil
}

type accessToken struct {
	uaaPublicKey string
}

func NewAccessToken(uaaPublicKey string) accessToken {
	return accessToken{
		uaaPublicKey: uaaPublicKey,
	}
}

func (accessToken accessToken) DecodeToken(userToken string, desiredPermissions ...string) error {
	userToken, err := checkTokenFormat(userToken)
	if err != nil {
		return err
	}

	token, err := jwt.Parse(userToken, func(t *jwt.Token) (interface{}, error) {
		return []byte(accessToken.uaaPublicKey), nil
	})

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

func (accessToken accessToken) CheckPublicToken() error {
	var block *pem.Block
	if block, _ = pem.Decode([]byte(accessToken.uaaPublicKey)); block == nil {
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
	if tokenType != "bearer" {
		return "", errors.New("Invalid token type: " + tokenType)
	}

	return userToken, nil
}
