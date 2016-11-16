package route_service_test

import (
	"Fmt"
	"errors"
	"net/http"
	"net/url"
	"time"

	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/common/secure/fakes"
	"code.cloudfoundry.org/gorouter/route_service"
	"code.cloudfoundry.org/gorouter/route_service/header"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Route Service Config", func() {
	var (
		config         *route_service.RouteServiceConfig
		crypto         secure.Crypto
		cryptoPrev     secure.Crypto
		cryptoKey      = "ABCDEFGHIJKLMNOP"
		logger         lager.Logger
		recommendHttps bool
	)

	BeforeEach(func() {
		var err error
		crypto, err = secure.NewAesGCM([]byte(cryptoKey))
		Expect(err).ToNot(HaveOccurred())
		logger = lagertest.NewTestLogger("test")
		config = route_service.NewRouteServiceConfig(logger, true, 1*time.Hour, crypto, cryptoPrev, recommendHttps)
	})

	AfterEach(func() {
		crypto = nil
		cryptoPrev = nil
		config = nil
	})

	Describe("GenerateSignatureAndMetadata", func() {
		It("decodes an encoded URL", func() {
			encodedForwardedURL := url.QueryEscape("test.app.com?query=sample")

			signatureHeader, metadataHeader, err := config.GenerateSignatureAndMetadata(encodedForwardedURL)
			Expect(err).ToNot(HaveOccurred())

			signature, err := header.SignatureFromHeaders(signatureHeader, metadataHeader, crypto)
			Expect(err).ToNot(HaveOccurred())

			Expect(signature.ForwardedUrl).ToNot(BeEmpty())
		})

		It("sets the requested time", func() {
			encodedForwardedURL := url.QueryEscape("test.app.com?query=sample")
			now := time.Now()

			signatureHeader, metadataHeader, err := config.GenerateSignatureAndMetadata(encodedForwardedURL)
			Expect(err).ToNot(HaveOccurred())

			signature, err := header.SignatureFromHeaders(signatureHeader, metadataHeader, crypto)
			Expect(err).ToNot(HaveOccurred())

			Expect(signature.RequestedTime).To(BeTemporally(">=", now))
		})

		It("returns an error if given an invalid encoded URL", func() {
			encodedForwardedURL := "test.app.com?query=sample%"

			signatureHeader, metadataHeader, err := config.GenerateSignatureAndMetadata(encodedForwardedURL)
			Expect(err).To(HaveOccurred())

			Expect(signatureHeader).To(BeEmpty())
			Expect(metadataHeader).To(BeEmpty())
		})

		Context("when encryption fails", func() {
			BeforeEach(func() {
				fakeCrypto := &fakes.FakeCrypto{}
				fakeCrypto.EncryptReturns([]byte{}, []byte{}, errors.New("test failed"))

				config = route_service.NewRouteServiceConfig(logger, true, 1*time.Hour, fakeCrypto, cryptoPrev, recommendHttps)
			})

			It("returns an error", func() {
				encodedForwardedURL := "test.app.com"

				signatureHeader, metadataHeader, err := config.GenerateSignatureAndMetadata(encodedForwardedURL)
				Expect(err).To(HaveOccurred())

				Expect(signatureHeader).To(BeEmpty())
				Expect(metadataHeader).To(BeEmpty())
			})
		})
	})

	Describe("SetupRouteServiceRequest", func() {
		var (
			request *http.Request
			rsArgs  route_service.RouteServiceArgs
		)

		BeforeEach(func() {
			request = test_util.NewRequest("GET", "test.com", "/path/", nil)
			str := "https://example-route-service.com"
			parsed, err := url.Parse(str)
			Expect(err).NotTo(HaveOccurred())
			rsArgs = route_service.RouteServiceArgs{
				UrlString:       str,
				ParsedUrl:       parsed,
				Signature:       "signature",
				Metadata:        "metadata",
				ForwardedUrlRaw: "http://test.com/path/",
				RecommendHttps:  true,
			}
		})

		It("sets the signature and metadata headers", func() {
			Expect(request.Header.Get(route_service.RouteServiceSignature)).To(Equal(""))
			Expect(request.Header.Get(route_service.RouteServiceMetadata)).To(Equal(""))

			config.SetupRouteServiceRequest(request, rsArgs)

			Expect(request.Header.Get(route_service.RouteServiceSignature)).To(Equal("signature"))
			Expect(request.Header.Get(route_service.RouteServiceMetadata)).To(Equal("metadata"))
		})

		It("sets the forwarded URL header", func() {
			Expect(request.Header.Get(route_service.RouteServiceForwardedUrl)).To(Equal(""))

			config.SetupRouteServiceRequest(request, rsArgs)

			Expect(request.Header.Get(route_service.RouteServiceForwardedUrl)).To(Equal("http://test.com/path/"))
		})

		It("changes the request host and URL", func() {
			config.SetupRouteServiceRequest(request, rsArgs)

			Expect(request.URL.Host).To(Equal("example-route-service.com"))
			Expect(request.URL.Scheme).To(Equal("https"))
		})

	})

	Describe("ValidateSignature", func() {
		var (
			signatureHeader string
			metadataHeader  string
			requestUrl      string
			headers         *http.Header
			signature       *header.Signature
		)

		BeforeEach(func() {
			h := make(http.Header, 0)
			headers = &h
			var err error
			requestUrl = "some-forwarded-url"
			signature = &header.Signature{
				RequestedTime: time.Now(),
				ForwardedUrl:  requestUrl,
			}
			signatureHeader, metadataHeader, err = header.BuildSignatureAndMetadata(crypto, signature)
			Expect(err).ToNot(HaveOccurred())

			headers.Set(route_service.RouteServiceForwardedUrl, "some-forwarded-url")
		})

		JustBeforeEach(func() {
			headers.Set(route_service.RouteServiceSignature, signatureHeader)
			headers.Set(route_service.RouteServiceMetadata, metadataHeader)
		})

		It("decrypts a valid signature", func() {
			err := config.ValidateSignature(headers, requestUrl)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("when the timestamp is expired", func() {
			BeforeEach(func() {
				signature = &header.Signature{
					RequestedTime: time.Now().Add(-10 * time.Hour),
					ForwardedUrl:  "some-forwarded-url",
				}
				var err error
				signatureHeader, metadataHeader, err = header.BuildSignatureAndMetadata(crypto, signature)
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns an route service request expired error", func() {
				err := config.ValidateSignature(headers, requestUrl)
				Expect(err).To(HaveOccurred())
				Expect(err).To(BeAssignableToTypeOf(route_service.RouteServiceExpired))
				Expect(err.Error()).To(ContainSubstring("request expired"))
			})
		})

		Context("when the signature is invalid", func() {
			BeforeEach(func() {
				signatureHeader = "zKQt4bnxW30Kxky"
				metadataHeader = "eyJpdiI6IjlBVn"
			})
			It("returns an error", func() {
				err := config.ValidateSignature(headers, requestUrl)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when the request URL is different from the signature", func() {
			BeforeEach(func() {
				requestUrl = "not-forwarded-url"
			})

			It("returns a route service request bad forwarded url error", func() {
				err := config.ValidateSignature(headers, requestUrl)
				Expect(err).To(HaveOccurred())
				Expect(err).To(BeAssignableToTypeOf(route_service.RouteServiceForwardedUrlMismatch))
			})
		})

		Context("when there is a url encoded character in the request", func() {
			encodedCharacters := make(map[string]string)
			encodedCharacters["%2C"] = ","
			encodedCharacters["%20"] = " "
			encodedCharacters["%41"] = "A"

			for encoded, decoded := range encodedCharacters {
				forwardedUrl := fmt.Sprintf("some-forwarded-url?fields=foo%sbar", decoded)
				url := fmt.Sprintf("?fields=foo%sbar", encoded)

				Context("with character "+decoded, func() {
					BeforeEach(func() {
						signature = &header.Signature{
							RequestedTime: time.Now(),
							ForwardedUrl:  forwardedUrl,
						}
						var err error
						signatureHeader, metadataHeader, err = header.BuildSignatureAndMetadata(crypto, signature)
						Expect(err).ToNot(HaveOccurred())
						requestUrl = requestUrl + url
					})

					It("decrypts the valid signature with character "+encoded, func() {
						err := config.ValidateSignature(headers, requestUrl)
						Expect(err).NotTo(HaveOccurred())
					})
				})
			}
		})

		Context("when there is a percent without two hexadec following in the url", func() {
			BeforeEach(func() {
				signature = &header.Signature{
					RequestedTime: time.Now(),
					ForwardedUrl:  "random%",
				}
				var err error
				signatureHeader, metadataHeader, err = header.BuildSignatureAndMetadata(crypto, signature)
				Expect(err).ToNot(HaveOccurred())
			})

			It("request url does not match forwarded url", func() {
				err := config.ValidateSignature(headers, "random%")
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when the header does not match the current key", func() {
			BeforeEach(func() {
				var err error
				crypto, err = secure.NewAesGCM([]byte("QRSTUVWXYZ123456"))
				Expect(err).NotTo(HaveOccurred())
				config = route_service.NewRouteServiceConfig(logger, true, 1*time.Hour, crypto, cryptoPrev, recommendHttps)
			})

			Context("when there is no previous key in the configuration", func() {
				It("rejects the signature", func() {
					err := config.ValidateSignature(headers, requestUrl)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("authentication failed"))
				})
			})

			Context("when the header key matches the previous key in the configuration", func() {
				BeforeEach(func() {
					var err error
					cryptoPrev, err = secure.NewAesGCM([]byte(cryptoKey))
					Expect(err).ToNot(HaveOccurred())
					config = route_service.NewRouteServiceConfig(logger, true, 1*time.Hour, crypto, cryptoPrev, recommendHttps)
				})

				It("validates the signature", func() {
					err := config.ValidateSignature(headers, requestUrl)
					Expect(err).NotTo(HaveOccurred())
				})

				Context("when a request has an expired Route service signature header", func() {
					BeforeEach(func() {
						signature = &header.Signature{
							RequestedTime: time.Now().Add(-10 * time.Hour),
							ForwardedUrl:  "some-forwarded-url",
						}
						var err error
						signatureHeader, metadataHeader, err = header.BuildSignatureAndMetadata(crypto, signature)
						Expect(err).ToNot(HaveOccurred())
					})

					It("returns an route service request expired error", func() {
						err := config.ValidateSignature(headers, requestUrl)
						Expect(err).To(HaveOccurred())
						Expect(err).To(BeAssignableToTypeOf(route_service.RouteServiceExpired))
					})
				})
			})

			Context("when the header key does not match the previous key in the configuration", func() {
				BeforeEach(func() {
					var err error
					cryptoPrev, err = secure.NewAesGCM([]byte("QRSTUVWXYZ123456"))
					Expect(err).ToNot(HaveOccurred())
					config = route_service.NewRouteServiceConfig(logger, true, 1*time.Hour, crypto, cryptoPrev, recommendHttps)
				})

				It("rejects the signature", func() {
					err := config.ValidateSignature(headers, requestUrl)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("authentication failed"))
				})
			})
		})
	})
})
