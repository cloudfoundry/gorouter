package handlers_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/errorwriter"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"

	fakeRegistry "code.cloudfoundry.org/gorouter/registry/fakes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/urfave/negroni/v3"
)

var _ = Describe("Route Service Handler", func() {
	var (
		handler *negroni.Negroni

		reg      *fakeRegistry.FakeRegistry
		routeMap map[string]*route.EndpointPool

		resp *httptest.ResponseRecorder
		req  *http.Request

		config       *routeservice.RouteServiceConfig
		crypto       *secure.AesGCM
		routePool    *route.EndpointPool
		forwardedUrl string

		ew = errorwriter.NewPlaintextErrorWriter()

		reqChan chan *http.Request

		nextCalled  bool
		prevHandler negroni.Handler
		logger      logger.Logger
	)

	nextHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, err := io.ReadAll(req.Body)
		Expect(err).NotTo(HaveOccurred())

		reqChan <- req
		rw.WriteHeader(http.StatusTeapot)
		rw.Write([]byte("I'm a little teapot, short and stout."))

		nextCalled = true
	})

	testSetupHandler := func(rw http.ResponseWriter, req *http.Request, next http.HandlerFunc) {
		reqInfo, err := handlers.ContextRequestInfo(req)
		Expect(err).ToNot(HaveOccurred())
		reqInfo.RoutePool = routePool
		next(rw, req)
	}

	BeforeEach(func() {
		body := bytes.NewBufferString("What are you?")
		testReq := test_util.NewRequest("GET", "my_host.com", "/resource+9-9_9?query=dog%0Acat&query$2=345#page1..5", body)
		forwardedUrl = "https://my_host.com/resource+9-9_9?query=dog%0Acat&query$2=345#page1..5"
		reqBuf := new(bytes.Buffer)
		err := testReq.Write(reqBuf)
		Expect(err).ToNot(HaveOccurred())
		req, err = http.ReadRequest(bufio.NewReader(reqBuf))
		Expect(err).ToNot(HaveOccurred())

		logger = test_util.NewTestZapLogger("test")

		resp = httptest.NewRecorder()

		reqChan = make(chan *http.Request, 1)

		routePool = route.NewPool(&route.PoolOpts{
			Logger:             logger,
			RetryAfterFailure:  1 * time.Second,
			Host:               "my_host.com",
			ContextPath:        "/resource+9-9_9",
			MaxConnsPerBackend: 0,
		})

		reg = &fakeRegistry.FakeRegistry{}
		routeMap = make(map[string]*route.EndpointPool)
		reg.LookupStub = func(uri route.Uri) *route.EndpointPool {
			return routeMap[uri.String()]
		}
		routeMap["my_host.com/resource+9-9_9"] = routePool

		crypto, err = secure.NewAesGCM([]byte("ABCDEFGHIJKLMNOP"))
		Expect(err).NotTo(HaveOccurred())
		config = routeservice.NewRouteServiceConfig(
			logger, true, true, nil, 60*time.Second, crypto, nil, true, false,
		)

		nextCalled = false
		prevHandler = &PrevHandler{}
	})

	AfterEach(func() {
		close(reqChan)
	})

	JustBeforeEach(func() {
		handler = negroni.New()
		handler.Use(handlers.NewRequestInfo())
		handler.UseFunc(testSetupHandler)
		handler.Use(prevHandler)
		handler.Use(handlers.NewRouteService(config, reg, logger, ew))
		handler.UseHandlerFunc(nextHandler)
	})

	Context("with route services disabled", func() {
		BeforeEach(func() {
			config = routeservice.NewRouteServiceConfig(logger, false, false, nil, 0, nil, nil, false, false)
		})

		Context("for normal routes", func() {
			BeforeEach(func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{})

				added := routePool.Put(endpoint)
				Expect(added).To(Equal(route.ADDED))
			})
			It("should not add route service metadata to the request for normal routes", func() {
				handler.ServeHTTP(resp, req)

				var passedReq *http.Request
				Eventually(reqChan).Should(Receive(&passedReq))

				Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).To(BeEmpty())
				Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).To(BeEmpty())
				Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(BeEmpty())

				reqInfo, err := handlers.ContextRequestInfo(passedReq)
				Expect(err).ToNot(HaveOccurred())
				Expect(reqInfo.RouteServiceURL).To(BeNil())
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})
		})

		Context("with route service URL configured for the route", func() {
			BeforeEach(func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{RouteServiceUrl: "route-service.com"})
				added := routePool.Put(endpoint)
				Expect(added).To(Equal(route.ADDED))
			})

			It("returns 502 Bad Gateway", func() {
				handler.ServeHTTP(resp, req)

				Expect(logger).To(gbytes.Say(`route-service-unsupported`))
				Expect(resp.Code).To(Equal(http.StatusBadGateway))
				Expect(resp.Header().Get("X-Cf-RouterError")).To(Equal(`route_service_unsupported`))
				Expect(resp.Body.String()).To(ContainSubstring(`Support for route services is disabled.`))
				Expect(nextCalled).To(BeFalse())
			})
		})
	})

	Context("with Route Services enabled", func() {
		Context("for normal routes", func() {
			BeforeEach(func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{})
				added := routePool.Put(endpoint)
				Expect(added).To(Equal(route.ADDED))
			})
			It("should not add route service metadata to the request for normal routes", func() {
				handler.ServeHTTP(resp, req)

				var passedReq *http.Request
				Eventually(reqChan).Should(Receive(&passedReq))

				Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).To(BeEmpty())
				Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).To(BeEmpty())
				Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(BeEmpty())
				reqInfo, err := handlers.ContextRequestInfo(passedReq)
				Expect(err).ToNot(HaveOccurred())
				Expect(reqInfo.RouteServiceURL).To(BeNil())
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			Context("with strictSignatureValidation enabled", func() {
				BeforeEach(func() {
					config = routeservice.NewRouteServiceConfig(
						logger, true, false, nil, 60*time.Second, crypto, nil, false, true,
					)
				})

				It("rejects all invalid signature headers", func() {
					req.Header.Add(routeservice.HeaderKeySignature, "invalid")
					handler.ServeHTTP(resp, req)
					Expect(resp.Code).To(Equal(http.StatusBadRequest))
					Expect(resp.Header().Get("X-Cf-RouterError")).To(Equal("invalid_route_service_signature"))
					Expect(resp.Body.String()).To(ContainSubstring("invalid route service signature detected"))
				})
			})

		})

		Context("with route service URL configured for the route", func() {
			BeforeEach(func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{RouteServiceUrl: "https://route-service.com"})
				added := routePool.Put(endpoint)
				Expect(added).To(Equal(route.ADDED))
			})

			It("sends the request to the route service with X-CF-Forwarded-Url using https scheme", func() {
				handler.ServeHTTP(resp, req)

				Expect(resp.Code).To(Equal(http.StatusTeapot))

				var passedReq *http.Request
				Eventually(reqChan).Should(Receive(&passedReq))

				Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).ToNot(BeEmpty())
				Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).ToNot(BeEmpty())
				Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(ContainSubstring("https://"))

				reqInfo, err := handlers.ContextRequestInfo(passedReq)
				Expect(err).ToNot(HaveOccurred())
				Expect(reqInfo.RouteServiceURL).ToNot(BeNil())

				Expect(reqInfo.RouteServiceURL.Host).To(Equal("route-service.com"))
				Expect(reqInfo.RouteServiceURL.Scheme).To(Equal("https"))
				Expect(reqInfo.ShouldRouteToInternalRouteService).To(BeFalse())
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			Context("when the route service has a route in the route registry", func() {
				BeforeEach(func() {
					rsPool := route.NewPool(&route.PoolOpts{
						Logger:             logger,
						RetryAfterFailure:  2 * time.Minute,
						Host:               "route-service.com",
						ContextPath:        "/",
						MaxConnsPerBackend: 0,
					})
					routeMap["route-service.com"] = rsPool
				})

				It("adds a flag to the request context", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusTeapot))

					var passedReq *http.Request
					Eventually(reqChan).Should(Receive(&passedReq))

					Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).ToNot(BeEmpty())
					Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).ToNot(BeEmpty())
					Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(ContainSubstring("https://"))

					reqInfo, err := handlers.ContextRequestInfo(passedReq)
					Expect(err).ToNot(HaveOccurred())
					Expect(reqInfo.RouteServiceURL).ToNot(BeNil())

					Expect(reqInfo.RouteServiceURL.Host).To(Equal("route-service.com"))
					Expect(reqInfo.RouteServiceURL.Scheme).To(Equal("https"))
					Expect(reqInfo.ShouldRouteToInternalRouteService).To(BeTrue())
					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				})

				Context("when the hairpin feature flag is disabled", func() {
					BeforeEach(func() {
						hairpinning := false
						config = routeservice.NewRouteServiceConfig(
							logger, true, hairpinning, nil, 60*time.Second, crypto, nil, true, false,
						)
					})

					It("does not add a flag to the request context", func() {
						handler.ServeHTTP(resp, req)

						Expect(resp.Code).To(Equal(http.StatusTeapot))

						var passedReq *http.Request
						Eventually(reqChan).Should(Receive(&passedReq))

						Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).ToNot(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).ToNot(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(ContainSubstring("https://"))

						reqInfo, err := handlers.ContextRequestInfo(passedReq)
						Expect(err).ToNot(HaveOccurred())
						Expect(reqInfo.RouteServiceURL).ToNot(BeNil())

						Expect(reqInfo.RouteServiceURL.Host).To(Equal("route-service.com"))
						Expect(reqInfo.RouteServiceURL.Scheme).To(Equal("https"))
						Expect(reqInfo.ShouldRouteToInternalRouteService).To(BeFalse())
						Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
					})

				})
				Context("when the hairpin feature flag is enabled", func() {
					BeforeEach(func() {
						hairpinning := true
						config = routeservice.NewRouteServiceConfig(
							logger, true, hairpinning, nil, 60*time.Second, crypto, nil, true, false,
						)
					})

					It("adds a flag to the request context", func() {
						handler.ServeHTTP(resp, req)

						Expect(resp.Code).To(Equal(http.StatusTeapot))

						var passedReq *http.Request
						Eventually(reqChan).Should(Receive(&passedReq))

						Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).ToNot(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).ToNot(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(ContainSubstring("https://"))

						reqInfo, err := handlers.ContextRequestInfo(passedReq)
						Expect(err).ToNot(HaveOccurred())
						Expect(reqInfo.RouteServiceURL).ToNot(BeNil())

						Expect(reqInfo.RouteServiceURL.Host).To(Equal("route-service.com"))
						Expect(reqInfo.RouteServiceURL.Scheme).To(Equal("https"))
						Expect(reqInfo.ShouldRouteToInternalRouteService).To(BeTrue())
						Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
					})

				})
				Context("when the hairpin feature flag is enabled with allowlist", func() {
					BeforeEach(func() {
						hairpinning := true
						config = routeservice.NewRouteServiceConfig(
							logger, true, hairpinning, []string{"route-service.com"}, 60*time.Second, crypto, nil, true, false,
						)
					})

					It("adds a flag to the request context", func() {
						handler.ServeHTTP(resp, req)

						Expect(resp.Code).To(Equal(http.StatusTeapot))

						var passedReq *http.Request
						Eventually(reqChan).Should(Receive(&passedReq))

						Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).ToNot(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).ToNot(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(ContainSubstring("https://"))

						reqInfo, err := handlers.ContextRequestInfo(passedReq)
						Expect(err).ToNot(HaveOccurred())
						Expect(reqInfo.RouteServiceURL).ToNot(BeNil())

						Expect(reqInfo.RouteServiceURL.Host).To(Equal("route-service.com"))
						Expect(reqInfo.RouteServiceURL.Scheme).To(Equal("https"))
						Expect(reqInfo.ShouldRouteToInternalRouteService).To(BeTrue())
						Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
					})

				})

				Context("when the hairpin feature flag is enabled, only with not matching allowlist entries", func() {
					BeforeEach(func() {
						hairpinning := true
						config = routeservice.NewRouteServiceConfig(
							logger, true, hairpinning, []string{"example.com"}, 60*time.Second, crypto, nil, true, false,
						)
					})

					It("does not add a flag to the request context", func() {
						handler.ServeHTTP(resp, req)

						Expect(resp.Code).To(Equal(http.StatusTeapot))

						var passedReq *http.Request
						Eventually(reqChan).Should(Receive(&passedReq))

						Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).ToNot(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).ToNot(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(ContainSubstring("https://"))

						reqInfo, err := handlers.ContextRequestInfo(passedReq)
						Expect(err).ToNot(HaveOccurred())
						Expect(reqInfo.RouteServiceURL).ToNot(BeNil())

						Expect(reqInfo.RouteServiceURL.Host).To(Equal("route-service.com"))
						Expect(reqInfo.RouteServiceURL.Scheme).To(Equal("https"))
						Expect(reqInfo.ShouldRouteToInternalRouteService).To(BeFalse())
						Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
					})

				})

				Context("when the hairpin feature flag is enabled, with a large list of not matching allowlist entries", func() {
					BeforeEach(func() {
						hairpinning := true
						config = routeservice.NewRouteServiceConfig(
							logger, true, hairpinning, generateHugeAllowlist(1000000), 60*time.Second, crypto, nil, true, false,
						)
					})

					It("does not add a flag to the request context", func() {

						start := time.Now()
						handler.ServeHTTP(resp, req)
						duration := time.Since(start)

						// This test does no warmup / cache and a single sample. Take with a grain of salt.
						fmt.Printf("Time taken to process request with large allowlist: %s", duration)

						Expect(resp.Code).To(Equal(http.StatusTeapot))

						var passedReq *http.Request
						Eventually(reqChan).Should(Receive(&passedReq))

						Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).ToNot(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).ToNot(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(ContainSubstring("https://"))

						reqInfo, err := handlers.ContextRequestInfo(passedReq)
						Expect(err).ToNot(HaveOccurred())
						Expect(reqInfo.RouteServiceURL).ToNot(BeNil())

						Expect(reqInfo.RouteServiceURL.Host).To(Equal("route-service.com"))
						Expect(reqInfo.RouteServiceURL.Scheme).To(Equal("https"))
						Expect(reqInfo.ShouldRouteToInternalRouteService).To(BeFalse())
						Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
					})

				})
			})

			Context("when recommendHttps is set to false", func() {
				BeforeEach(func() {
					config = routeservice.NewRouteServiceConfig(
						logger, true, false, nil, 60*time.Second, crypto, nil, false, false,
					)
				})
				It("sends the request to the route service with X-CF-Forwarded-Url using http scheme", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusTeapot))

					var passedReq *http.Request
					Eventually(reqChan).Should(Receive(&passedReq))

					Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).ToNot(BeEmpty())
					Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).ToNot(BeEmpty())
					Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(ContainSubstring("http://"))

					reqInfo, err := handlers.ContextRequestInfo(passedReq)
					Expect(err).ToNot(HaveOccurred())
					Expect(reqInfo.RouteServiceURL).ToNot(BeNil())

					Expect(reqInfo.RouteServiceURL.Host).To(Equal("route-service.com"))
					Expect(reqInfo.RouteServiceURL.Scheme).To(Equal("https"))
					Expect(reqInfo.ShouldRouteToInternalRouteService).To(BeFalse())
					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				})
			})

			Context("when a request has a valid route service signature and metadata header", func() {
				BeforeEach(func() {
					reqArgs, err := config.CreateRequest("", forwardedUrl)
					Expect(err).ToNot(HaveOccurred())
					req.Header.Set(routeservice.HeaderKeySignature, reqArgs.Signature)
					req.Header.Set(routeservice.HeaderKeyMetadata, reqArgs.Metadata)
				})

				It("strips headers and sends the request to the backend instance", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusTeapot))

					var passedReq *http.Request
					Eventually(reqChan).Should(Receive(&passedReq))

					Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).To(BeEmpty())
					Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).To(BeEmpty())
					Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(BeEmpty())
					reqInfo, err := handlers.ContextRequestInfo(passedReq)
					Expect(err).ToNot(HaveOccurred())
					Expect(reqInfo.RouteServiceURL).To(BeNil())
					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				})
			})

			Context("when a request has a valid route service signature and metadata header and URL contains special chars", func() {
				BeforeEach(func() {
					reqArgs, err := config.CreateRequest("", "https://my_host.com/resource+9-9_9?query=%23%25")
					Expect(err).ToNot(HaveOccurred())
					req.Header.Set(routeservice.HeaderKeySignature, reqArgs.Signature)
					req.Header.Set(routeservice.HeaderKeyMetadata, reqArgs.Metadata)
				})

				It("should get response from backend instance without errors", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusTeapot))
				})
			})

			Context("when a request has a route service signature but no metadata header", func() {
				BeforeEach(func() {
					reqArgs, err := config.CreateRequest("", forwardedUrl)
					Expect(err).ToNot(HaveOccurred())
					req.Header.Set(routeservice.HeaderKeySignature, reqArgs.Signature)
				})

				It("returns a 502 bad gateway response", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusBadGateway))
					Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))
					Expect(logger).To(gbytes.Say(`signature-validation-failed`))

					Expect(nextCalled).To(BeFalse())
				})
			})

			Context("when a request has an expired route service signature header", func() {
				BeforeEach(func() {
					signature := &routeservice.SignatureContents{
						RequestedTime: time.Now().Add(-2 * time.Minute),
						ForwardedUrl:  forwardedUrl,
					}

					signatureHeader, metadataHeader, err := routeservice.BuildSignatureAndMetadata(crypto, signature)
					Expect(err).ToNot(HaveOccurred())
					req.Header.Set(routeservice.HeaderKeySignature, signatureHeader)
					req.Header.Set(routeservice.HeaderKeyMetadata, metadataHeader)
				})

				It("returns a 504 gateway timeout response", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusGatewayTimeout))
					Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))
					Expect(logger).To(gbytes.Say(`signature-validation-failed`))

					Expect(nextCalled).To(BeFalse())
				})
			})

			Context("when the signature's forwarded_url does not match the request", func() {
				BeforeEach(func() {
					reqArgs, err := config.CreateRequest("", "https://my_host.com/original_path")
					Expect(err).ToNot(HaveOccurred())
					req.Header.Set(routeservice.HeaderKeySignature, reqArgs.Signature)
					req.Header.Set(routeservice.HeaderKeyMetadata, reqArgs.Metadata)

					rsPool := route.NewPool(&route.PoolOpts{
						Logger:             logger,
						RetryAfterFailure:  2 * time.Minute,
						Host:               "my_host.com",
						ContextPath:        "/original_path",
						MaxConnsPerBackend: 0,
					})
					routeMap["my_host.com/original_path"] = rsPool
				})

				It("returns a 502 bad gateway response", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusBadGateway))
					Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))
					Expect(logger).To(gbytes.Say(`signature-validation-failed`))

					Expect(nextCalled).To(BeFalse())
				})
			})

			Context("when a request header key does not match the crypto key in the config", func() {
				BeforeEach(func() {
					signature := &routeservice.SignatureContents{
						RequestedTime: time.Now(),
						ForwardedUrl:  forwardedUrl,
					}

					altCrypto, err := secure.NewAesGCM([]byte("QRSTUVWXYZ123456"))
					Expect(err).NotTo(HaveOccurred())

					signatureHeader, metadataHeader, err := routeservice.BuildSignatureAndMetadata(altCrypto, signature)
					Expect(err).ToNot(HaveOccurred())
					req.Header.Set(routeservice.HeaderKeySignature, signatureHeader)
					req.Header.Set(routeservice.HeaderKeyMetadata, metadataHeader)
				})

				It("returns a 502 bad gateway response", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusBadGateway))
					Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))
					Expect(logger).To(gbytes.Say(`signature-validation-failed`))

					Expect(nextCalled).To(BeFalse())
				})
			})

			Context("with a previous crypto key in the config", func() {
				var cryptoPrev *secure.AesGCM
				BeforeEach(func() {
					var err error
					cryptoPrev, err = secure.NewAesGCM([]byte("QRSTUVWXYZ123456"))
					Expect(err).ToNot(HaveOccurred())
					config = routeservice.NewRouteServiceConfig(
						logger, true, false, nil, 60*time.Second, crypto, cryptoPrev, true, false,
					)
				})

				Context("when a request header key matches the previous crypto key in the config", func() {
					BeforeEach(func() {
						signature := &routeservice.SignatureContents{
							RequestedTime: time.Now(),
							ForwardedUrl:  forwardedUrl,
						}

						signatureHeader, metadataHeader, err := routeservice.BuildSignatureAndMetadata(cryptoPrev, signature)
						Expect(err).ToNot(HaveOccurred())
						req.Header.Set(routeservice.HeaderKeySignature, signatureHeader)
						req.Header.Set(routeservice.HeaderKeyMetadata, metadataHeader)
					})

					It("sends the request to the backend instance", func() {
						handler.ServeHTTP(resp, req)

						var passedReq *http.Request
						Eventually(reqChan).Should(Receive(&passedReq))

						Expect(passedReq.Header.Get(routeservice.HeaderKeySignature)).To(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyMetadata)).To(BeEmpty())
						Expect(passedReq.Header.Get(routeservice.HeaderKeyForwardedURL)).To(BeEmpty())
						reqInfo, err := handlers.ContextRequestInfo(passedReq)
						Expect(err).ToNot(HaveOccurred())
						Expect(reqInfo.RouteServiceURL).To(BeNil())
						Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
					})
				})

				Context("when a request has an expired route service signature header", func() {
					BeforeEach(func() {
						signature := &routeservice.SignatureContents{
							RequestedTime: time.Now().Add(-2 * time.Minute),
							ForwardedUrl:  forwardedUrl,
						}

						signatureHeader, metadataHeader, err := routeservice.BuildSignatureAndMetadata(cryptoPrev, signature)
						Expect(err).ToNot(HaveOccurred())
						req.Header.Set(routeservice.HeaderKeySignature, signatureHeader)
						req.Header.Set(routeservice.HeaderKeyMetadata, metadataHeader)
					})

					It("returns a 504 gateway timeout response", func() {
						handler.ServeHTTP(resp, req)

						Expect(resp.Code).To(Equal(http.StatusGatewayTimeout))
						Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))
						Expect(logger).To(gbytes.Say(`signature-validation-failed`))

						Expect(nextCalled).To(BeFalse())
					})
				})

				Context("when a request header key does not match the previous crypto key in the config", func() {
					BeforeEach(func() {
						signature := &routeservice.SignatureContents{
							RequestedTime: time.Now(),
							ForwardedUrl:  forwardedUrl,
						}

						altCrypto, err := secure.NewAesGCM([]byte("123456QRSTUVWXYZ"))
						Expect(err).NotTo(HaveOccurred())

						signatureHeader, metadataHeader, err := routeservice.BuildSignatureAndMetadata(altCrypto, signature)
						Expect(err).ToNot(HaveOccurred())
						req.Header.Set(routeservice.HeaderKeySignature, signatureHeader)
						req.Header.Set(routeservice.HeaderKeyMetadata, metadataHeader)
					})

					It("returns a 400 bad request response", func() {
						handler.ServeHTTP(resp, req)

						Expect(resp.Code).To(Equal(http.StatusBadGateway))
						Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))

						Expect(nextCalled).To(BeFalse())
					})
				})
			})
		})

		Context("when a websocket request an app bound to a route service", func() {
			BeforeEach(func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{RouteServiceUrl: "https://goodrouteservice.com"})

				added := routePool.Put(endpoint)
				Expect(added).To(Equal(route.ADDED))
				req.Header.Set("connection", "upgrade")
				req.Header.Set("upgrade", "websocket")

			})
			It("returns a 503", func() {
				handler.ServeHTTP(resp, req)

				Expect(resp.Code).To(Equal(http.StatusServiceUnavailable))
				Expect(resp.Body.String()).To(ContainSubstring("Websocket requests are not supported for routes bound to Route Services."))

				Expect(nextCalled).To(BeFalse())
			})
		})

		Context("when a bad route service url is used", func() {
			BeforeEach(func() {
				endpoint := route.NewEndpoint(&route.EndpointOpts{RouteServiceUrl: "https://bad%20service.com"})
				added := routePool.Put(endpoint)
				Expect(added).To(Equal(route.ADDED))

			})
			It("returns a 500 internal server error response", func() {
				handler.ServeHTTP(resp, req)

				Expect(resp.Code).To(Equal(http.StatusInternalServerError))
				Expect(resp.Body.String()).To(ContainSubstring("Route service request failed."))

				Expect(nextCalled).To(BeFalse())
			})
		})
	})

	Context("when request info is not set on the request context", func() {
		var badHandler *negroni.Negroni
		BeforeEach(func() {
			badHandler = negroni.New()
			badHandler.Use(handlers.NewRouteService(config, reg, logger, ew))
			badHandler.UseHandlerFunc(nextHandler)
		})
		It("calls Panic on the logger", func() {
			defer func() {
				recover()
				Expect(logger).To(gbytes.Say(`request-info-err`))
				Expect(nextCalled).To(BeFalse())
			}()
			badHandler.ServeHTTP(resp, req)
		})
	})

	Context("when request info is set on the request context without route pool", func() {
		var badHandler *negroni.Negroni
		BeforeEach(func() {
			badHandler = negroni.New()
			badHandler.Use(handlers.NewRequestInfo())
			badHandler.Use(handlers.NewRouteService(config, reg, logger, ew))
			badHandler.UseHandlerFunc(nextHandler)
		})
		It("calls Panic on the logger", func() {
			defer func() {
				recover()
				Expect(logger).To(gbytes.Say(`failed-to-access-RoutePool`))
				Expect(nextCalled).To(BeFalse())
			}()
			badHandler.ServeHTTP(resp, req)
		})
	})
	Context("allowlist wildcards resolve correctly", func() {

		type testcase struct {
			name      string
			allowlist []string
			host      string
			matched   bool
			err       bool
		}
		tests := []testcase{
			{
				name:      "Test invalid wildcard leading with 2 subdomains",
				allowlist: []string{"*.*.wildcard-a.com"},
				host:      "first.authentication.wildcard-a.com",
				matched:   false,
				err:       true,
			},
			{
				name:      "Test wildcard in the wrong position",
				allowlist: []string{"first.*.wildcard-a.com"},
				host:      "first.authentication.wildcard-a.com",
				matched:   false,
				err:       true,
			},
			{
				name:      "Test wildcard domain without path",
				allowlist: []string{"*.wildcard-a.com"},
				host:      "authentication.wildcard-a.com",
				matched:   true,
				err:       false,
			},
			{
				name:      "Test wildcard domain is not a part of other domain",
				allowlist: []string{"*.wildcard-a.com"},
				host:      "cola-wildcard-a.com",
				matched:   false,
				err:       false,
			},
			{
				name:      "Test wildcard for subdomain",
				allowlist: []string{"*.authentication.wildcard-a.com"},
				host:      "first.authentication.wildcard-a.com",
				matched:   true,
				err:       false,
			},
			{
				name:      "Test wildcard for subdomain with CamelCase",
				allowlist: []string{"*.authentication.wildcard-a.com"},
				host:      "First.Authentication.Wildcard-A.com",
				matched:   true,
				err:       false,
			},
			{
				name:      "Test wildcard for subdomain with CamelCase in allowlist",
				allowlist: []string{"*.Authentication.Wildcard-A.com"},
				host:      "first.authentication.wildcard-a.com",
				matched:   true,
				err:       false,
			},
			{
				name:      "Test wildcard for wrong domain on subdomain",
				allowlist: []string{"*.authentication.wildcard-a.com"},
				host:      "first.authentication-wildcard-a.com",
				matched:   false,
				err:       false,
			},
			{
				name:      "Test fixed host name",
				allowlist: []string{"authentication.wildcard-a.com"},
				host:      "authentication.wildcard-a.com",
				matched:   true,
				err:       false,
			},
			{
				name:      "Test fixed host name with CamelCase",
				allowlist: []string{"authentication.wildcard-a.com"},
				host:      "Authentication.Wildcard-A.com",
				matched:   true,
				err:       false,
			},
			{
				name:      "Test fixed host name with CamelCase in allowlist",
				allowlist: []string{"Authentication.Wildcard-A.com"},
				host:      "authentication.wildcard-a.com",
				matched:   true,
				err:       false,
			},
			{
				name:      "Test wrong fixed host name",
				allowlist: []string{"authentication.wildcard-a.com"},
				host:      "first.authentication.wildcard-a.com",
				matched:   false,
				err:       false,
			},
			{
				name:      "Test injecting a regex",
				allowlist: []string{"(.|\\w)+"},
				host:      "first.authentication.wildcard-a.com",
				matched:   false,
				err:       true,
			},
			{
				name:      "Test bad allowlist entry",
				allowlist: []string{"*.ba d.cöm"},
				host:      "host.bad.com",
				matched:   false,
				err:       true,
			},
			{
				name:      "Subdomain wildcard should not match domain w/o segment covered by the wildcard",
				allowlist: []string{"*.authentication.wildcard-a.com"},
				host:      "authentication.wildcard-a.com",
				matched:   false,
				err:       false,
			},
		}

		It("for the pattern", func() {
			for _, testCase := range tests {
				By(testCase.name)

				config = routeservice.NewRouteServiceConfig(
					logger, true, true, testCase.allowlist, 60*time.Second, crypto, nil, true, false,
				)

				if testCase.err {
					defer func() {
						recover()
						Expect(logger).To(gbytes.Say(`allowlist-entry-invalid`))
					}()
					handlers.NewRouteService(config, reg, logger, ew)
					continue
				}

				r := handlers.NewRouteService(config, reg, logger, ew).(*handlers.RouteService)

				matched := r.MatchAllowlistHostname(testCase.host)
				Expect(matched).To(Equal(testCase.matched))
			}
		})
	})
})

func generateHugeAllowlist(size int) []string {
	buffer := make([]string, 0, size)

	for i := 0; i < size; i++ {
		buffer = append(buffer, fmt.Sprintf("*.subdomain-%d.example.com", i))
	}

	return buffer
}
