package routeservice_test

import (
	"errors"
	"log/slog"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"go.uber.org/zap/zapcore"

	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/common/secure/fakes"
	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"
)

var _ = Describe("Route Service Config", func() {
	var (
		config           *routeservice.RouteServiceConfig
		crypto           secure.Crypto
		cryptoPrev       secure.Crypto
		cryptoKey        = "ABCDEFGHIJKLMNOP"
		testSink         *test_util.TestSink
		logger           *slog.Logger
		recommendHttps   bool
		strictValidation bool
	)

	BeforeEach(func() {
		var err error
		crypto, err = secure.NewAesGCM([]byte(cryptoKey))
		Expect(err).ToNot(HaveOccurred())
		logger = log.CreateLogger()
		testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
		log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
		log.SetLoggingLevel("Debug")
		config = routeservice.NewRouteServiceConfig(logger, true, true, nil, 1*time.Hour, crypto, cryptoPrev, recommendHttps, strictValidation)
	})

	AfterEach(func() {
		crypto = nil
		cryptoPrev = nil
		config = nil
	})

	Describe("Request", func() {
		It("sets the signature to the forwarded URL exactly", func() {
			rawForwardedURL := "this is my url%0A"
			rsUrl := "https://example.com"

			args, err := config.CreateRequest(rsUrl, rawForwardedURL)
			Expect(err).NotTo(HaveOccurred())
			Expect(args.ForwardedURL).To(Equal(rawForwardedURL))

			signatureContents, err := routeservice.SignatureContentsFromHeaders(args.Signature, args.Metadata, crypto)
			Expect(err).ToNot(HaveOccurred())

			Expect(signatureContents.ForwardedUrl).To(Equal(rawForwardedURL))
		})

		It("sets the requested time", func() {
			rawForwardedUrl := "test.app.com?query=sample"
			now := time.Now()
			rsUrl := "https://example.com"

			args, err := config.CreateRequest(rsUrl, rawForwardedUrl)
			Expect(err).NotTo(HaveOccurred())

			signatureContents, err := routeservice.SignatureContentsFromHeaders(args.Signature, args.Metadata, crypto)
			Expect(err).ToNot(HaveOccurred())

			Expect(signatureContents.RequestedTime).To(BeTemporally(">=", now))
		})

		Context("when encryption fails", func() {
			BeforeEach(func() {
				fakeCrypto := &fakes.FakeCrypto{}
				fakeCrypto.EncryptReturns([]byte{}, []byte{}, errors.New("test failed"))

				config = routeservice.NewRouteServiceConfig(logger, true, false, nil, 1*time.Hour, fakeCrypto, cryptoPrev, recommendHttps, strictValidation)
			})

			It("returns an error", func() {
				rawForwardedURL := "test.app.com"
				rsUrl := "https://example.com"

				args, err := config.CreateRequest(rsUrl, rawForwardedURL)
				Expect(err).To(HaveOccurred())

				Expect(args.Metadata).To(BeEmpty())
				Expect(args.Signature).To(BeEmpty())
			})
		})

		It("returns route service request information", func() {
			rsUrl := "https://example.com"
			forwardedUrl := "https://forwarded.example.com"
			args, err := config.CreateRequest(rsUrl, forwardedUrl)
			Expect(err).NotTo(HaveOccurred())

			rsURL, err := url.Parse(rsUrl)
			Expect(err).ToNot(HaveOccurred())

			Expect(args.ParsedUrl).To(Equal(rsURL))
			Expect(args.URLString).To(Equal(rsUrl))
			Expect(args.ForwardedURL).To(Equal(forwardedUrl))
		})
	})

	Describe("ValidateRequest", func() {
		var (
			requestFromRouteService routeservice.RequestReceivedFromRouteService
			requestUrl              string
			signatureContents       *routeservice.SignatureContents
		)

		BeforeEach(func() {
			var err error
			requestUrl = "http://some-forwarded-url.com"
			signatureContents = &routeservice.SignatureContents{
				RequestedTime: time.Now(),
				ForwardedUrl:  requestUrl,
			}

			signature, metadata, err := routeservice.BuildSignatureAndMetadata(crypto, signatureContents)
			Expect(err).ToNot(HaveOccurred())

			requestFromRouteService = routeservice.RequestReceivedFromRouteService{
				AppUrl:    requestUrl,
				Signature: signature,
				Metadata:  metadata,
			}
		})

		It("decrypts a valid signature and returns the decrypted signature", func() {
			validatedSig, err := config.ValidateRequest(requestFromRouteService)
			Expect(err).NotTo(HaveOccurred())
			Expect(validatedSig.ForwardedUrl).To(Equal(signatureContents.ForwardedUrl))
			Expect(validatedSig.RequestedTime.Equal(signatureContents.RequestedTime)).To(BeTrue())
		})

		Context("when the timestamp is expired", func() {
			BeforeEach(func() {
				signatureContents = &routeservice.SignatureContents{
					RequestedTime: time.Now().Add(-10 * time.Hour),
					ForwardedUrl:  requestUrl,
				}
				signature, metadata, err := routeservice.BuildSignatureAndMetadata(crypto, signatureContents)
				Expect(err).ToNot(HaveOccurred())

				requestFromRouteService = routeservice.RequestReceivedFromRouteService{
					AppUrl:    requestUrl,
					Signature: signature,
					Metadata:  metadata,
				}
			})

			It("returns an route service request expired error", func() {
				_, err := config.ValidateRequest(requestFromRouteService)
				Expect(err).To(HaveOccurred())
				Expect(err).To(BeAssignableToTypeOf(routeservice.ErrExpired))
				Expect(err.Error()).To(ContainSubstring("request expired"))
			})
		})

		Context("when the signature is invalid", func() {
			BeforeEach(func() {
				requestFromRouteService = routeservice.RequestReceivedFromRouteService{
					AppUrl:    requestUrl,
					Signature: "zKQt4bnxW30Kxky",
					Metadata:  "eyJpdiI6IjlBVn",
				}
			})

			It("returns an error", func() {
				_, err := config.ValidateRequest(requestFromRouteService)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when the header does not match the current key", func() {
			BeforeEach(func() {
				var err error
				crypto, err = secure.NewAesGCM([]byte("QRSTUVWXYZ123456"))
				Expect(err).NotTo(HaveOccurred())
				config = routeservice.NewRouteServiceConfig(logger, true, false, nil, 1*time.Hour, crypto, cryptoPrev, recommendHttps, strictValidation)
			})

			Context("when there is no previous key in the configuration", func() {
				It("rejects the signature", func() {
					_, err := config.ValidateRequest(requestFromRouteService)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("authentication failed"))
				})
			})

			Context("when the header key matches the previous key in the configuration", func() {
				BeforeEach(func() {
					var err error
					cryptoPrev, err = secure.NewAesGCM([]byte(cryptoKey))
					Expect(err).ToNot(HaveOccurred())
					config = routeservice.NewRouteServiceConfig(logger, true, false, nil, 1*time.Hour, crypto, cryptoPrev, recommendHttps, strictValidation)
				})

				It("validates the signature", func() {
					_, err := config.ValidateRequest(requestFromRouteService)
					Expect(err).NotTo(HaveOccurred())
				})

				Context("when a request has an expired Route service signature header", func() {
					BeforeEach(func() {
						signatureContents = &routeservice.SignatureContents{
							RequestedTime: time.Now().Add(-10 * time.Hour),
							ForwardedUrl:  "some-forwarded-url",
						}
						signature, metadata, err := routeservice.BuildSignatureAndMetadata(crypto, signatureContents)
						Expect(err).ToNot(HaveOccurred())

						requestFromRouteService = routeservice.RequestReceivedFromRouteService{
							AppUrl:    requestUrl,
							Signature: signature,
							Metadata:  metadata,
						}
					})

					It("returns an route service request expired error", func() {
						_, err := config.ValidateRequest(requestFromRouteService)
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
					config = routeservice.NewRouteServiceConfig(logger, true, false, nil, 1*time.Hour, crypto, cryptoPrev, recommendHttps, strictValidation)
				})

				It("rejects the signature", func() {
					_, err := config.ValidateRequest(requestFromRouteService)
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
				config = routeservice.NewRouteServiceConfig(logger, true, true, nil, 1*time.Hour, crypto, cryptoPrev, recommendHttps, strictValidation)
			})

			It("returns the routeServiceEnabled to be true", func() {
				Expect(config.RouteServiceRecommendHttps()).To(BeTrue())
			})
		})

		Context("when rs recommendHttps is set to false", func() {
			BeforeEach(func() {
				recommendHttps = false
				config = routeservice.NewRouteServiceConfig(logger, true, true, nil, 1*time.Hour, crypto, cryptoPrev, recommendHttps, strictValidation)
			})

			It("returns the routeServiceEnabled to be false", func() {
				Expect(config.RouteServiceRecommendHttps()).To(BeFalse())
			})
		})
	})

	Describe("RouteServiceHairpinning", func() {
		Context("when routeServiceHairpinning is set to true", func() {
			BeforeEach(func() {
				recommendHttps = true
				config = routeservice.NewRouteServiceConfig(logger, true, true, nil, 1*time.Hour, crypto, cryptoPrev, recommendHttps, strictValidation)
			})

			It("returns the routeServiceEnabled to be true", func() {
				Expect(config.RouteServiceHairpinning()).To(BeTrue())
			})
		})

		Context("when routeServiceHairpinning is set to false", func() {
			BeforeEach(func() {
				recommendHttps = false
				config = routeservice.NewRouteServiceConfig(logger, true, false, nil, 1*time.Hour, crypto, cryptoPrev, recommendHttps, strictValidation)
			})

			It("returns the routeServiceHairpinning to be false", func() {
				Expect(config.RouteServiceHairpinning()).To(BeFalse())
			})
		})
	})

	Describe("RouteServiceEnabled", func() {
		Context("when  RouteService is Enabled", func() {
			BeforeEach(func() {
				routeServiceEnabled := true
				config = routeservice.NewRouteServiceConfig(logger, routeServiceEnabled, false, nil, 1*time.Hour, crypto, cryptoPrev, recommendHttps, strictValidation)
			})

			It("returns the routeServiceEnabled to be true", func() {
				Expect(config.RouteServiceEnabled()).To(BeTrue())
			})
		})

		Context("when  RouteService is not Enabled", func() {
			BeforeEach(func() {
				routeServiceEnabled := false
				config = routeservice.NewRouteServiceConfig(logger, routeServiceEnabled, false, nil, 1*time.Hour, crypto, cryptoPrev, recommendHttps, strictValidation)
			})

			It("returns the routeServiceEnabled to be false", func() {
				Expect(config.RouteServiceEnabled()).To(BeFalse())
			})
		})
	})
})
