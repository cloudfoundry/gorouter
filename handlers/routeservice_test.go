package handlers_test

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/routing-api/models"

	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"
	fakeRegistry "code.cloudfoundry.org/gorouter/registry/fakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("Route Service Handler", func() {
	var (
		handler *negroni.Negroni

		reg      *fakeRegistry.FakeRegistry
		routeMap map[string]*route.Pool

		resp *httptest.ResponseRecorder
		req  *http.Request

		config       *routeservice.RouteServiceConfig
		crypto       *secure.AesGCM
		routePool    *route.Pool
		forwardedUrl string

		fakeLogger *logger_fakes.FakeLogger

		reqChan chan *http.Request

		nextCalled bool
	)

	nextHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		_, err := ioutil.ReadAll(req.Body)
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
		testReq := test_util.NewRequest("GET", "my_host.com", "/resource+9-9_9?query=123&query$2=345#page1..5", body)
		forwardedUrl = "https://my_host.com/resource+9-9_9?query=123&query$2=345#page1..5"
		reqBuf := new(bytes.Buffer)
		err := testReq.Write(reqBuf)
		Expect(err).ToNot(HaveOccurred())
		req, err = http.ReadRequest(bufio.NewReader(reqBuf))
		Expect(err).ToNot(HaveOccurred())

		resp = httptest.NewRecorder()

		reqChan = make(chan *http.Request, 1)

		routePool = route.NewPool(1*time.Second, "my_host.com", "/resource+9-9_9")

		fakeLogger = new(logger_fakes.FakeLogger)
		reg = &fakeRegistry.FakeRegistry{}
		routeMap = make(map[string]*route.Pool)
		reg.LookupStub = func(uri route.Uri) *route.Pool {
			return routeMap[uri.String()]
		}
		routeMap["my_host.com/resource%209-9_9"] = routePool

		crypto, err = secure.NewAesGCM([]byte("ABCDEFGHIJKLMNOP"))
		Expect(err).NotTo(HaveOccurred())
		config = routeservice.NewRouteServiceConfig(
			fakeLogger, true, 60*time.Second, crypto, nil, true,
		)

		nextCalled = false
	})

	AfterEach(func() {
		close(reqChan)
	})

	JustBeforeEach(func() {
		handler = negroni.New()
		handler.Use(handlers.NewRequestInfo())
		handler.UseFunc(testSetupHandler)
		handler.Use(handlers.NewRouteService(config, fakeLogger, reg))
		handler.UseHandlerFunc(nextHandler)
	})

	Context("with route services disabled", func() {
		BeforeEach(func() {
			config = routeservice.NewRouteServiceConfig(fakeLogger, false, 0, nil, nil, false)
		})

		Context("for normal routes", func() {
			BeforeEach(func() {
				endpoint := route.NewEndpoint("appId", "1.1.1.1", uint16(9090), "id", "1",
					map[string]string{}, 0, "", models.ModificationTag{}, "", false)

				added := routePool.Put(endpoint)
				Expect(added).To(BeTrue())
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
				endpoint := route.NewEndpoint("appId", "1.1.1.1", uint16(9090), "id", "1",
					map[string]string{}, 0, "route-service.com", models.ModificationTag{}, "", false)

				added := routePool.Put(endpoint)
				Expect(added).To(BeTrue())
			})

			It("returns 502 Bad Gateway", func() {
				handler.ServeHTTP(resp, req)

				Expect(fakeLogger.InfoCallCount()).ToNot(Equal(0))
				message, _ := fakeLogger.InfoArgsForCall(0)
				Expect(message).To(Equal(`route-service-unsupported`))
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
				endpoint := route.NewEndpoint("appId", "1.1.1.1", uint16(9090), "id", "1",
					map[string]string{}, 0, "", models.ModificationTag{}, "", false)

				added := routePool.Put(endpoint)
				Expect(added).To(BeTrue())
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
				endpoint := route.NewEndpoint(
					"appId", "1.1.1.1", uint16(9090), "id", "1", map[string]string{}, 0,
					"https://route-service.com", models.ModificationTag{}, "", false,
				)
				added := routePool.Put(endpoint)
				Expect(added).To(BeTrue())
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
				Expect(reqInfo.IsInternalRouteService).To(BeFalse())
				Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
			})

			Context("when the route service has a route in the route registry", func() {
				BeforeEach(func() {
					rsPool := route.NewPool(2*time.Minute, "route-service.com", "/")
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
					Expect(reqInfo.IsInternalRouteService).To(BeTrue())
					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				})
			})

			Context("when recommendHttps is set to false", func() {
				BeforeEach(func() {
					config = routeservice.NewRouteServiceConfig(
						fakeLogger, true, 60*time.Second, crypto, nil, false,
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
					Expect(reqInfo.IsInternalRouteService).To(BeFalse())
					Expect(nextCalled).To(BeTrue(), "Expected the next handler to be called.")
				})
			})

			Context("when a request has a valid route service signature and metadata header", func() {
				BeforeEach(func() {
					reqArgs, err := config.Request("", forwardedUrl)
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

			Context("when a request has a route service signature but no metadata header", func() {
				BeforeEach(func() {
					reqArgs, err := config.Request("", forwardedUrl)
					Expect(err).ToNot(HaveOccurred())
					req.Header.Set(routeservice.HeaderKeySignature, reqArgs.Signature)
				})

				It("returns a 400 bad request response", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusBadRequest))
					Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))
					Expect(fakeLogger.ErrorCallCount()).To(Equal(2))
					errMsg, _ := fakeLogger.ErrorArgsForCall(1)
					Expect(errMsg).To(Equal("signature-validation-failed"))

					Expect(nextCalled).To(BeFalse())
				})
			})

			Context("when a request has an expired route service signature header", func() {
				BeforeEach(func() {
					decodedURL, err := url.QueryUnescape(forwardedUrl)
					Expect(err).ToNot(HaveOccurred())

					signature := &routeservice.Signature{
						RequestedTime: time.Now().Add(-2 * time.Minute),
						ForwardedUrl:  decodedURL,
					}

					signatureHeader, metadataHeader, err := routeservice.BuildSignatureAndMetadata(crypto, signature)
					Expect(err).ToNot(HaveOccurred())
					req.Header.Set(routeservice.HeaderKeySignature, signatureHeader)
					req.Header.Set(routeservice.HeaderKeyMetadata, metadataHeader)
				})

				It("returns a 400 bad request response", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusBadRequest))
					Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))
					Expect(fakeLogger.ErrorCallCount()).To(Equal(2))
					errMsg, _ := fakeLogger.ErrorArgsForCall(1)
					Expect(errMsg).To(Equal("signature-validation-failed"))

					Expect(nextCalled).To(BeFalse())
				})
			})

			Context("when the signature's forwarded_url does not match the request", func() {
				BeforeEach(func() {
					reqArgs, err := config.Request("", "https://my_host.com/original_path")
					Expect(err).ToNot(HaveOccurred())
					req.Header.Set(routeservice.HeaderKeySignature, reqArgs.Signature)
					req.Header.Set(routeservice.HeaderKeyMetadata, reqArgs.Metadata)

					rsPool := route.NewPool(2*time.Minute, "my_host.com", "/original_path")
					routeMap["my_host.com/original_path"] = rsPool
				})

				It("returns a 400 bad request response", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusBadRequest))
					Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))
					Expect(fakeLogger.ErrorCallCount()).To(Equal(1))
					errMsg, _ := fakeLogger.ErrorArgsForCall(0)
					Expect(errMsg).To(Equal("signature-validation-failed"))

					Expect(nextCalled).To(BeFalse())
				})
			})

			Context("when a request header key does not match the crypto key in the config", func() {
				BeforeEach(func() {
					decodedURL, err := url.QueryUnescape(forwardedUrl)
					Expect(err).ToNot(HaveOccurred())

					signature := &routeservice.Signature{
						RequestedTime: time.Now(),
						ForwardedUrl:  decodedURL,
					}

					altCrypto, err := secure.NewAesGCM([]byte("QRSTUVWXYZ123456"))
					Expect(err).NotTo(HaveOccurred())

					signatureHeader, metadataHeader, err := routeservice.BuildSignatureAndMetadata(altCrypto, signature)
					Expect(err).ToNot(HaveOccurred())
					req.Header.Set(routeservice.HeaderKeySignature, signatureHeader)
					req.Header.Set(routeservice.HeaderKeyMetadata, metadataHeader)
				})

				It("returns a 400 bad request response", func() {
					handler.ServeHTTP(resp, req)

					Expect(resp.Code).To(Equal(http.StatusBadRequest))
					Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))
					Expect(fakeLogger.ErrorCallCount()).To(Equal(2))
					errMsg, _ := fakeLogger.ErrorArgsForCall(1)
					Expect(errMsg).To(Equal("signature-validation-failed"))

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
						fakeLogger, true, 60*time.Second, crypto, cryptoPrev, true,
					)
				})

				Context("when a request header key matches the previous crypto key in the config", func() {
					BeforeEach(func() {
						decodedURL, err := url.QueryUnescape(forwardedUrl)
						Expect(err).ToNot(HaveOccurred())

						signature := &routeservice.Signature{
							RequestedTime: time.Now(),
							ForwardedUrl:  decodedURL,
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
						decodedURL, err := url.QueryUnescape(forwardedUrl)
						Expect(err).ToNot(HaveOccurred())

						signature := &routeservice.Signature{
							RequestedTime: time.Now().Add(-2 * time.Minute),
							ForwardedUrl:  decodedURL,
						}

						signatureHeader, metadataHeader, err := routeservice.BuildSignatureAndMetadata(cryptoPrev, signature)
						Expect(err).ToNot(HaveOccurred())
						req.Header.Set(routeservice.HeaderKeySignature, signatureHeader)
						req.Header.Set(routeservice.HeaderKeyMetadata, metadataHeader)
					})

					It("returns a 400 bad request response", func() {
						handler.ServeHTTP(resp, req)

						Expect(resp.Code).To(Equal(http.StatusBadRequest))
						Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))
						Expect(fakeLogger.ErrorCallCount()).To(Equal(2))

						errMsg, _ := fakeLogger.ErrorArgsForCall(1)
						Expect(errMsg).To(Equal("signature-validation-failed"))

						Expect(nextCalled).To(BeFalse())
					})
				})

				Context("when a request header key does not match the previous crypto key in the config", func() {
					BeforeEach(func() {
						decodedURL, err := url.QueryUnescape(forwardedUrl)
						Expect(err).ToNot(HaveOccurred())

						signature := &routeservice.Signature{
							RequestedTime: time.Now(),
							ForwardedUrl:  decodedURL,
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

						Expect(resp.Code).To(Equal(http.StatusBadRequest))
						Expect(resp.Body.String()).To(ContainSubstring("Failed to validate Route Service Signature"))

						Expect(nextCalled).To(BeFalse())
					})
				})
			})
		})

		Context("when a TCP request an app bound to a route service", func() {
			BeforeEach(func() {
				endpoint := route.NewEndpoint(
					"appId", "1.1.1.1", uint16(9090), "id", "1", map[string]string{}, 0,
					"https://goodrouteservice.com", models.ModificationTag{}, "", false,
				)

				added := routePool.Put(endpoint)
				Expect(added).To(BeTrue())
				req.Header.Set("connection", "upgrade")
				req.Header.Set("upgrade", "tcp")

			})
			It("returns a 503", func() {
				handler.ServeHTTP(resp, req)

				Expect(resp.Code).To(Equal(http.StatusServiceUnavailable))
				Expect(resp.Body.String()).To(ContainSubstring("TCP requests are not supported for routes bound to Route Services."))

				Expect(nextCalled).To(BeFalse())
			})
		})
		Context("when a websocket request an app bound to a route service", func() {
			BeforeEach(func() {
				endpoint := route.NewEndpoint(
					"appId", "1.1.1.1", uint16(9090), "id", "1", map[string]string{}, 0,
					"https://goodrouteservice.com", models.ModificationTag{}, "", false,
				)

				added := routePool.Put(endpoint)
				Expect(added).To(BeTrue())
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
				endpoint := route.NewEndpoint(
					"appId", "1.1.1.1", uint16(9090), "id", "1", map[string]string{}, 0,
					"https://bad%20service.com", models.ModificationTag{}, "", false,
				)

				added := routePool.Put(endpoint)
				Expect(added).To(BeTrue())

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
			badHandler.Use(handlers.NewRouteService(config, fakeLogger, reg))
			badHandler.UseHandlerFunc(nextHandler)
		})
		It("calls Fatal on the logger", func() {
			badHandler.ServeHTTP(resp, req)
			Expect(fakeLogger.FatalCallCount()).To(Equal(1))
			Expect(nextCalled).To(BeFalse())
		})
	})

	Context("when request info is not set on the request context", func() {
		var badHandler *negroni.Negroni
		BeforeEach(func() {
			badHandler = negroni.New()
			badHandler.Use(handlers.NewRequestInfo())
			badHandler.Use(handlers.NewRouteService(config, fakeLogger, reg))
			badHandler.UseHandlerFunc(nextHandler)
		})
		It("calls Fatal on the logger", func() {
			badHandler.ServeHTTP(resp, req)
			Expect(fakeLogger.FatalCallCount()).To(Equal(1))
			Expect(nextCalled).To(BeFalse())
		})
	})
})
