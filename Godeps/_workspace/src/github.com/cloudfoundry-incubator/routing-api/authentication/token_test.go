package authentication_test

import (
	"time"

	"github.com/cloudfoundry-incubator/routing-api/authentication"
	"github.com/cloudfoundry-incubator/routing-api/authentication/fakes"
	"github.com/dgrijalva/jwt-go"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Token", func() {
	var (
		accessToken authentication.Token

		signedKey      string
		UserPrivateKey string
		UAAPublicKey   string

		token *jwt.Token
		err   error
	)

	BeforeEach(func() {
		UserPrivateKey = "UserPrivateKey"
		UAAPublicKey = "UAAPublicKey"

		fakes.RegisterFastTokenSigningMethod()

		header := map[string]interface{}{
			"alg": "FAST",
		}

		alg := "FAST"
		signingMethod := jwt.GetSigningMethod(alg)
		token = jwt.New(signingMethod)
		token.Header = header

		accessToken = authentication.NewAccessToken(UAAPublicKey)
	})

	Describe(".DecodeToken", func() {
		Context("when the token is valid", func() {
			BeforeEach(func() {
				claims := map[string]interface{}{
					"exp":   3404281214,
					"scope": []string{"route.advertise"},
				}
				token.Claims = claims

				signedKey, err = token.SignedString([]byte(UserPrivateKey))
				Expect(err).NotTo(HaveOccurred())

				signedKey = "bearer " + signedKey
			})

			It("does not return an error", func() {
				err := accessToken.DecodeToken(signedKey, "route.advertise")
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when a token is not valid", func() {
			It("returns an error if the user token is not signed", func() {
				err = accessToken.DecodeToken("bearer not-a-signed-key", "not a permission")
				Expect(err).To(HaveOccurred())
			})

			It("returns an invalid token format when there is no token type", func() {
				err = accessToken.DecodeToken("has-no-token-type", "not a permission")

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("Invalid token format"))
			})

			It("returns an invalid token type when type is not bearer", func() {
				err = accessToken.DecodeToken("basic some-auth", "not a permission")

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("Invalid token type: basic"))
			})
		})

		Context("expired time", func() {
			BeforeEach(func() {
				claims := map[string]interface{}{
					"exp": time.Now().Unix() - 5,
				}
				token.Claims = claims

				signedKey, err = token.SignedString([]byte(UserPrivateKey))
				Expect(err).NotTo(HaveOccurred())

				signedKey = "bearer " + signedKey
			})

			It("returns an error if the token is expired", func() {
				err = accessToken.DecodeToken(signedKey, "route.advertise")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("token is expired"))
			})
		})

		Context("permissions", func() {
			BeforeEach(func() {
				claims := map[string]interface{}{
					"exp":   time.Now().Unix() + 50000000,
					"scope": []string{"route.foo"},
				}
				token.Claims = claims

				signedKey, err = token.SignedString([]byte(UserPrivateKey))
				Expect(err).NotTo(HaveOccurred())

				signedKey = "bearer " + signedKey
			})

			It("returns an error if the the user does not have requested permissions", func() {
				err = accessToken.DecodeToken(signedKey, "route.my-permissions", "some.other.scope")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("Token does not have 'route.my-permissions', 'some.other.scope' scope"))
			})
		})
	})

	Describe(".CheckPublicToken", func() {
		BeforeEach(func() {
			accessToken = authentication.NewAccessToken("not a valid pem string")
		})

		It("returns an error if the public token is malformed", func() {
			err = accessToken.CheckPublicToken()
			Expect(err).To(HaveOccurred())
		})
	})
})
