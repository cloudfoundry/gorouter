package fakes

import (
	"errors"

	"github.com/dgrijalva/jwt-go"
)

// Fake out jwt signing interface with these methods
func RegisterFastTokenSigningMethod() {
	jwt.RegisterSigningMethod("FAST", func() jwt.SigningMethod {
		return SigningMethodFast{}
	})
}

type SigningMethodFast struct{}

func (m SigningMethodFast) Alg() string {
	return "FAST"
}

func (m SigningMethodFast) Sign(signingString string, key interface{}) (string, error) {
	signature := jwt.EncodeSegment([]byte(signingString + "SUPERFAST"))
	return signature, nil
}

func (m SigningMethodFast) Verify(signingString, signature string, key interface{}) (err error) {
	if signature != jwt.EncodeSegment([]byte(signingString+"SUPERFAST")) {
		return errors.New("Signature is invalid")
	}

	return nil
}
