package routeservice_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/common/secure/fakes"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Route Service Config", func() {
	var (
		config         *routeservice.RouteServiceConfig
		crypto         secure.Crypto
		cryptoPrev     secure.Crypto
		cryptoKey      = "ABCDEFGHIJKLMNOP"
		logger         logger.Logger
		recommendHttps bool
	)

	BeforeEach(func() {
		var err error
		crypto, err = secure.NewAesGCM([]byte(cryptoKey))
		Expect(err).ToNot(HaveOccurred())
		logger = test_util.NewTestZapLogger("test")
		config = routeservice.NewRouteServiceConfig(logger, true, 1*time.Hour, crypto, cryptoPrev, recommendHttps)
	})

	AfterEach(func() {
		crypto = nil
		cryptoPrev = nil
		config = nil
	})

	Describe("Request", func() {
		It("decodes an encoded URL", func() {
			encodedForwardedURL := url.QueryEscape("test.app.com?query=sample")
			rsUrl := "https://example.com"

			args, err := config.Request(rsUrl, encodedForwardedURL)
			Expect(err).NotTo(HaveOccurred())

			signature, err := routeservice.SignatureFromHeaders(args.Signature, args.Metadata, crypto)
			Expect(err).ToNot(HaveOccurred())

			Expect(signature.ForwardedUrl).ToNot(BeEmpty())
		})

		It("sets the requested time", func() {
			encodedForwardedURL := url.QueryEscape("test.app.com?query=sample")
			now := time.Now()
			rsUrl := "https://example.com"

			args, err := config.Request(rsUrl, encodedForwardedURL)
			Expect(err).NotTo(HaveOccurred())

			signature, err := routeservice.SignatureFromHeaders(args.Signature, args.Metadata, crypto)
			Expect(err).ToNot(HaveOccurred())

			Expect(signature.RequestedTime).To(BeTemporally(">=", now))
		})

		It("returns an error if given an invalid encoded URL", func() {
			encodedForwardedURL := "test.app.com?query=sample%"
			rsUrl := "https://example.com"

			args, err := config.Request(rsUrl, encodedForwardedURL)
			Expect(err).To(HaveOccurred())

			Expect(args.Metadata).To(BeEmpty())
			Expect(args.Signature).To(BeEmpty())
		})

		Context("when encryption fails", func() {
			BeforeEach(func() {
				fakeCrypto := &fakes.FakeCrypto{}
				fakeCrypto.EncryptReturns([]byte{}, []byte{}, errors.New("test failed"))

				config = routeservice.NewRouteServiceConfig(logger, true, 1*time.Hour, fakeCrypto, cryptoPrev, recommendHttps)
			})

			It("returns an error", func() {
				encodedForwardedURL := "test.app.com"
				rsUrl := "https://example.com"

				args, err := config.Request(rsUrl, encodedForwardedURL)
				Expect(err).To(HaveOccurred())

				Expect(args.Metadata).To(BeEmpty())
				Expect(args.Signature).To(BeEmpty())
			})
		})

		It("returns route service request information", func() {
			rsUrl := "https://example.com"
			forwardedUrl := "https://forwarded.example.com"
			args, err := config.Request(rsUrl, forwardedUrl)
			Expect(err).NotTo(HaveOccurred())

			rsURL, err := url.Parse(rsUrl)
			Expect(err).ToNot(HaveOccurred())

			Expect(args.ParsedUrl).To(Equal(rsURL))
			Expect(args.URLString).To(Equal(rsUrl))
			Expect(args.ForwardedURL).To(Equal(fmt.Sprintf("%s", forwardedUrl)))
		})
	})

	Describe("ValidatedSignature", func() {
		var (
			signatureHeader string
			metadataHeader  string
			requestUrl      string
			headers         *http.Header
			signature       *routeservice.Signature
		)

		BeforeEach(func() {
			h := make(http.Header, 0)
			headers = &h
			var err error
			requestUrl = "http://some-forwarded-url.com"
			signature = &routeservice.Signature{
				RequestedTime: time.Now(),
				ForwardedUrl:  requestUrl,
			}
			signatureHeader, metadataHeader, err = routeservice.BuildSignatureAndMetadata(crypto, signature)
			Expect(err).ToNot(HaveOccurred())

			headers.Set(routeservice.HeaderKeyForwardedURL, requestUrl)
		})

		JustBeforeEach(func() {
			headers.Set(routeservice.HeaderKeySignature, signatureHeader)
			headers.Set(routeservice.HeaderKeyMetadata, metadataHeader)
		})

		It("decrypts a valid signature and returns the decrypted signature", func() {
			validatedSig, err := config.ValidatedSignature(headers, requestUrl)
			Expect(err).NotTo(HaveOccurred())
			Expect(validatedSig.ForwardedUrl).To(Equal(signature.ForwardedUrl))
			Expect(validatedSig.RequestedTime.String()).To(Equal(signature.RequestedTime.String()))
		})

		Context("when the timestamp is expired", func() {
			BeforeEach(func() {
				signature = &routeservice.Signature{
					RequestedTime: time.Now().Add(-10 * time.Hour),
					ForwardedUrl:  requestUrl,
				}
				var err error
				signatureHeader, metadataHeader, err = routeservice.BuildSignatureAndMetadata(crypto, signature)
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns an route service request expired error", func() {
				_, err := config.ValidatedSignature(headers, requestUrl)
				Expect(err).To(HaveOccurred())
				Expect(err).To(BeAssignableToTypeOf(routeservice.ErrExpired))
				Expect(err.Error()).To(ContainSubstring("request expired"))
			})
		})

		Context("when the signature is invalid", func() {
			BeforeEach(func() {
				signatureHeader = "zKQt4bnxW30Kxky"
				metadataHeader = "eyJpdiI6IjlBVn"
			})
			It("returns an error", func() {
				_, err := config.ValidatedSignature(headers, requestUrl)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when the header does not match the current key", func() {
			BeforeEach(func() {
				var err error
				crypto, err = secure.NewAesGCM([]byte("QRSTUVWXYZ123456"))
				Expect(err).NotTo(HaveOccurred())
				config = routeservice.NewRouteServiceConfig(logger, true, 1*time.Hour, crypto, cryptoPrev, recommendHttps)
			})

			Context("when there is no previous key in the configuration", func() {
				It("rejects the signature", func() {
					_, err := config.ValidatedSignature(headers, requestUrl)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("authentication failed"))
				})
			})

			Context("when the header key matches the previous key in the configuration", func() {
				BeforeEach(func() {
					var err error
					cryptoPrev, err = secure.NewAesGCM([]byte(cryptoKey))
					Expect(err).ToNot(HaveOccurred())
					config = routeservice.NewRouteServiceConfig(logger, true, 1*time.Hour, crypto, cryptoPrev, recommendHttps)
				})

				It("validates the signature", func() {
					_, err := config.ValidatedSignature(headers, requestUrl)
					Expect(err).NotTo(HaveOccurred())
				})

				Context("when a request has an expired Route service signature header", func() {
					BeforeEach(func() {
						signature = &routeservice.Signature{
							RequestedTime: time.Now().Add(-10 * time.Hour),
							ForwardedUrl:  "some-forwarded-url",
						}
						var err error
						signatureHeader, metadataHeader, err = routeservice.BuildSignatureAndMetadata(crypto, signature)
						Expect(err).ToNot(HaveOccurred())
					})

					It("returns an route service request expired error", func() {
						_, err := config.ValidatedSignature(headers, requestUrl)
						Expect(err).To(HaveOccurred())
						Expect(err).To(BeAssignableToTypeOf(routeservice.ErrExpired))
					})
				})
			})

			Context("when the header key does not match the previous key in the configuration", func() {
				BeforeEach(func() {
					var err error
					cryptoPrev, err = secure.NewAesGCM([]byte("QRSTUVWXYZ123456"))
					Expect(err).ToNot(HaveOccurred())
					config = routeservice.NewRouteServiceConfig(logger, true, 1*time.Hour, crypto, cryptoPrev, recommendHttps)
				})

				It("rejects the signature", func() {
					_, err := config.ValidatedSignature(headers, requestUrl)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("authentication failed"))
				})
			})
		})
	})

	Describe("RouteServiceEnabled", func() {
		Context("when rs recommendHttps is set to true", func() {
			BeforeEach(func() {
				recommendHttps = true
				config = routeservice.NewRouteServiceConfig(logger, true, 1*time.Hour,
					crypto, cryptoPrev, recommendHttps)
			})

			It("returns the routeServiceEnabled to be true", func() {
				Expect(config.RouteServiceRecommendHttps()).To(BeTrue())
			})
		})

		Context("when rs recommendHttps is set to false", func() {
			BeforeEach(func() {
				recommendHttps = false
				config = routeservice.NewRouteServiceConfig(logger, true, 1*time.Hour,
					crypto, cryptoPrev, recommendHttps)
			})

			It("returns the routeServiceEnabled to be false", func() {
				Expect(config.RouteServiceRecommendHttps()).To(BeFalse())
			})
		})
	})

	Describe("RouteServiceEnabled", func() {
		Context("when  RouteService is Enabled", func() {
			BeforeEach(func() {
				routeServiceEnabled := true
				config = routeservice.NewRouteServiceConfig(logger, routeServiceEnabled, 1*time.Hour,
					crypto, cryptoPrev, recommendHttps)
			})

			It("returns the routeServiceEnabled to be true", func() {
				Expect(config.RouteServiceEnabled()).To(BeTrue())
			})
		})

		Context("when  RouteService is not Enabled", func() {
			BeforeEach(func() {
				routeServiceEnabled := false
				config = routeservice.NewRouteServiceConfig(logger, routeServiceEnabled, 1*time.Hour,
					crypto, cryptoPrev, recommendHttps)
			})

			It("returns the routeServiceEnabled to be false", func() {
				Expect(config.RouteServiceEnabled()).To(BeFalse())
			})
		})
	})
})
