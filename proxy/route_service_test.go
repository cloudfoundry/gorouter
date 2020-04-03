package proxy_test

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net"
	"net/http"
	"sync"
	"time"

	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func HaveErrored() types.GomegaMatcher {
	return HaveOccurred()
}

var _ = Describe("Route Services", func() {
	var (
		routeServiceListener net.Listener
		routeServiceURL      string
		routeServiceHandler  func(rw http.ResponseWriter, req *http.Request)
		signatureHeader      string
		metadataHeader       string
		cryptoKey            = "ABCDEFGHIJKLMNOP"
		forwardedUrl         string
		rsCertChain          test_util.CertChain
		routeServiceServer   sync.WaitGroup
	)

	JustBeforeEach(func() {
		server := &http.Server{Handler: http.HandlerFunc(routeServiceHandler)}
		routeServiceServer.Add(1)
		go func() {
			defer routeServiceServer.Done()
			_ = server.Serve(routeServiceListener)
		}()
	})

	BeforeEach(func() {
		conf.RouteServiceEnabled = true
		recommendHttps = true
		forwardedUrl = "https://my_host.com/resource+9-9_9?query=123&query$2=345#page1..5"

		routeServiceHandler = func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Host).ToNot(Equal("my_host.com"))
			metaHeader := r.Header.Get(routeservice.HeaderKeyMetadata)
			sigHeader := r.Header.Get(routeservice.HeaderKeySignature)

			crypto, err := secure.NewAesGCM([]byte(cryptoKey))
			Expect(err).ToNot(HaveOccurred())
			_, err = routeservice.SignatureContentsFromHeaders(sigHeader, metaHeader, crypto)

			Expect(err).ToNot(HaveOccurred())
			Expect(r.Header.Get("X-CF-ApplicationID")).To(Equal(""))

			// validate client request header
			Expect(r.Header.Get("X-CF-Forwarded-Url")).To(Equal(forwardedUrl))

			_, err = w.Write([]byte("My Special Snowflake Route Service\n"))
			Expect(err).ToNot(HaveOccurred())
		}

		crypto, err := secure.NewAesGCM([]byte(cryptoKey))
		Expect(err).ToNot(HaveOccurred())

		config := routeservice.NewRouteServiceConfig(
			testLogger,
			conf.RouteServiceEnabled,
			conf.RouteServicesHairpinning,
			1*time.Hour,
			crypto,
			nil,
			recommendHttps,
		)
		reqArgs, err := config.CreateRequest("", forwardedUrl)
		Expect(err).ToNot(HaveOccurred())
		signatureHeader, metadataHeader = reqArgs.Signature, reqArgs.Metadata

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())

		rsCertNames := test_util.CertNames{
			CommonName: "route-service",
			SANs: test_util.SubjectAltNames{
				IP: "127.0.0.1",
			},
		}
		rsCertChain = test_util.CreateSignedCertWithRootCA(rsCertNames)
		routeServiceListener = tls.NewListener(ln, rsCertChain.AsTLSConfig())
		routeServiceURL = "https://" + routeServiceListener.Addr().String()
	})

	AfterEach(func() {
		err := routeServiceListener.Close()
		Expect(err).ToNot(HaveOccurred())
		routeServiceServer.Wait()
	})

	Context("with Route Services disabled", func() {
		BeforeEach(func() {
			conf.RouteServiceEnabled = false
			conf.SkipSSLValidation = true
			routeServiceHandler = func(http.ResponseWriter, *http.Request) {
				defer GinkgoRecover()
				Fail("Should not get here into Route Service")
			}
		})

		It("return 502 Bad Gateway", func() {
			ln := test_util.RegisterHandler(r, "my_host.com", func(conn *test_util.HttpConn) {
				defer GinkgoRecover()
				Fail("Should not get here into the app")
			}, test_util.RegisterConfig{RouteServiceUrl: routeServiceURL})
			defer func() {
				Expect(ln.Close()).ToNot(HaveErrored())
			}()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "my_host.com", "/", nil)

			conn.WriteRequest(req)

			res, body := conn.ReadResponse()
			Expect(res.StatusCode).To(Equal(http.StatusBadGateway))
			Expect(body).To(ContainSubstring("Support for route services is disabled."))
		})
	})

	Context("with SkipSSLValidation enabled", func() {
		BeforeEach(func() {
			conf.SkipSSLValidation = true
		})

		Context("when a request does not have a valid Route service signature header", func() {
			It("redirects the request to the route service url", func() {
				ln := test_util.RegisterHandler(r, "my_host.com", func(conn *test_util.HttpConn) {
					defer GinkgoRecover()
					Fail("Should not get here")
				}, test_util.RegisterConfig{RouteServiceUrl: routeServiceURL})
				defer func() {
					Expect(ln.Close()).ToNot(HaveErrored())
				}()

				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "my_host.com", "/resource+9-9_9?query=123&query$2=345#page1..5", nil)

				conn.WriteRequest(req)

				res, body := conn.ReadResponse()
				Expect(res.StatusCode).To(Equal(http.StatusOK))
				Expect(body).To(ContainSubstring("My Special Snowflake Route Service"))
			})

			Context("when the route service is not available", func() {
				It("returns a 502 bad gateway error", func() {
					ln := test_util.RegisterHandler(r, "my_host.com", func(conn *test_util.HttpConn) {
						defer GinkgoRecover()
						Fail("Should not get here")
					}, test_util.RegisterConfig{RouteServiceUrl: "https://bad-route-service"})
					defer func() {
						Expect(ln.Close()).ToNot(HaveErrored())
					}()

					conn := dialProxy(proxyServer)

					req := test_util.NewRequest("GET", "my_host.com", "/resource+9-9_9?query=123&query$2=345#page1..5", nil)

					conn.WriteRequest(req)

					res, _ := conn.ReadResponse()
					Expect(res.StatusCode).To(Equal(http.StatusBadGateway))
				})
			})
		})

		Context("when a request has a valid Route service signature header", func() {
			BeforeEach(func() {
				routeServiceHandler = func(http.ResponseWriter, *http.Request) {
					defer GinkgoRecover()
					Fail("Should not get here into Route Service")
				}
			})

			It("routes to the backend instance and strips headers", func() {
				ln := test_util.RegisterHandler(r, "my_host.com", func(conn *test_util.HttpConn) {
					req, _ := conn.ReadRequest()
					Expect(req.Header.Get(routeservice.HeaderKeySignature)).To(Equal(""))
					Expect(req.Header.Get(routeservice.HeaderKeyMetadata)).To(Equal(""))
					Expect(req.Header.Get(routeservice.HeaderKeyForwardedURL)).To(Equal(""))

					out := &bytes.Buffer{}
					out.WriteString("backend instance")
					res := &http.Response{
						StatusCode: http.StatusOK,
						Body:       ioutil.NopCloser(out),
					}
					conn.WriteResponse(res)
					conn.Close()
				}, test_util.RegisterConfig{RouteServiceUrl: routeServiceURL})
				defer func() {
					Expect(ln.Close()).ToNot(HaveErrored())
				}()

				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "my_host.com", "/resource+9-9_9?query=123&query$2=345#page1..5", nil)
				req.Header.Set(routeservice.HeaderKeySignature, signatureHeader)
				req.Header.Set(routeservice.HeaderKeyMetadata, metadataHeader)
				req.Header.Set(routeservice.HeaderKeyForwardedURL, "http://some-backend-url")
				conn.WriteRequest(req)

				res, body := conn.ReadResponse()
				Expect(body).To(ContainSubstring("backend instance"))
				Expect(res.StatusCode).To(Equal(http.StatusOK))
			})

			Context("when request has Host header with a port", func() {
				It("routes to backend instance and disregards port in Host header", func() {
					ln := test_util.RegisterHandler(r, "my_host.com", func(conn *test_util.HttpConn) {
						conn.ReadRequest()
						out := &bytes.Buffer{}
						out.WriteString("backend instance")
						res := &http.Response{
							StatusCode: http.StatusOK,
							Body:       ioutil.NopCloser(out),
						}
						conn.WriteResponse(res)
						conn.Close()
					}, test_util.RegisterConfig{RouteServiceUrl: routeServiceURL})
					defer func() {
						Expect(ln.Close()).ToNot(HaveErrored())
					}()

					conn := dialProxy(proxyServer)

					req := test_util.NewRequest("GET", "my_host.com", "/resource+9-9_9?query=123&query$2=345#page1..5", nil)
					req.Host = "my_host.com:4444"
					req.Header.Set(routeservice.HeaderKeySignature, signatureHeader)
					req.Header.Set(routeservice.HeaderKeyMetadata, metadataHeader)
					req.Header.Set(routeservice.HeaderKeyForwardedURL, "http://some-backend-url")
					conn.WriteRequest(req)

					res, body := conn.ReadResponse()
					Expect(body).To(ContainSubstring("backend instance"))
					Expect(res.StatusCode).To(Equal(http.StatusOK))
				})
			})

			Context("and is forwarding to a route service on CF", func() {
				It("does not strip the signature header", func() {
					ln := test_util.RegisterHandler(r, "my_host.com", func(conn *test_util.HttpConn) {
						req, _ := conn.ReadRequest()
						Expect(req.Header.Get(routeservice.HeaderKeySignature)).To(Equal("some-signature"))

						out := &bytes.Buffer{}
						out.WriteString("route service instance")
						res := &http.Response{
							StatusCode: http.StatusOK,
							Body:       ioutil.NopCloser(out),
						}
						conn.WriteResponse(res)
						conn.Close()
					})
					defer func() {
						Expect(ln.Close()).ToNot(HaveErrored())
					}()

					conn := dialProxy(proxyServer)

					req := test_util.NewRequest("GET", "my_host.com", "/resource+9-9_9?query=123&query$2=345#page1..5", nil)
					req.Header.Set(routeservice.HeaderKeySignature, "some-signature")
					conn.WriteRequest(req)

					res, body := conn.ReadResponse()
					Expect(res.StatusCode).To(Equal(http.StatusOK))
					Expect(body).To(ContainSubstring("route service instance"))
				})
			})

			It("returns 502 when backend not available", func() {
				// register route service, should NOT route to it
				test_util.RegisterAddr(r, "my_host.com", "localhost:81", test_util.RegisterConfig{
					RouteServiceUrl: routeServiceURL,
					InstanceId:      "instanceId",
					InstanceIndex:   "1",
				})

				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "my_host.com", "/resource+9-9_9?query=123&query$2=345#page1..5", nil)
				req.Header.Set(routeservice.HeaderKeySignature, signatureHeader)
				req.Header.Set(routeservice.HeaderKeyMetadata, metadataHeader)
				conn.WriteRequest(req)
				resp, _ := conn.ReadResponse()

				Expect(resp.StatusCode).To(Equal(http.StatusBadGateway))
			})
		})

		Context("when recommendHttps is set to false", func() {
			BeforeEach(func() {
				recommendHttps = false
				routeServiceHandler = func(w http.ResponseWriter, r *http.Request) {
					Expect(r.Header.Get("X-CF-Forwarded-Url")).To(ContainSubstring("http://"))

					_, err := w.Write([]byte("My Special Snowflake Route Service\n"))
					Expect(err).ToNot(HaveOccurred())
				}
			})

			It("routes to backend over http scheme", func() {
				ln := test_util.RegisterHandler(r, "my_host.com", func(conn *test_util.HttpConn) {
					defer GinkgoRecover()
					Fail("Should not get here")
				}, test_util.RegisterConfig{RouteServiceUrl: routeServiceURL})
				defer func() {
					Expect(ln.Close()).ToNot(HaveErrored())
				}()

				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "my_host.com", "/resource+9-9_9?query=123&query$2=345#page1..5", nil)
				conn.WriteRequest(req)

				res, body := conn.ReadResponse()
				Expect(body).To(ContainSubstring("My Special Snowflake Route Service"))
				Expect(res.StatusCode).To(Equal(http.StatusOK))
			})
		})
	})

	Context("when the SSL cert of the route service is signed by an unknown authority", func() {
		// the caCertPool is empty
		BeforeEach(func() {
			caCertPool = x509.NewCertPool()
		})

		It("returns a 526", func() {
			ln := test_util.RegisterHandler(r, "my_host.com", func(conn *test_util.HttpConn) {
				defer GinkgoRecover()
				Fail("Should not get here")
			}, test_util.RegisterConfig{RouteServiceUrl: routeServiceURL})
			defer func() {
				Expect(ln.Close()).ToNot(HaveErrored())
			}()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "my_host.com", "/resource+9-9_9?query=123&query$2=345#page1..5", nil)
			conn.WriteRequest(req)

			res := readResponseNoErrorCheck(conn)

			Expect(res.StatusCode).To(Equal(526))
		})
	})

	Context("with a valid certificate", func() {
		BeforeEach(func() {
			caCertPool = x509.NewCertPool()
			caCertPool.AddCert(rsCertChain.CACert)
		})

		It("returns a 200 when we route to a route service", func() {
			ln := test_util.RegisterHandler(r, "my_host.com", func(conn *test_util.HttpConn) {
				defer GinkgoRecover()
				Fail("Should not get here")
			}, test_util.RegisterConfig{RouteServiceUrl: routeServiceURL})
			defer func() {
				Expect(ln.Close()).ToNot(HaveErrored())
			}()

			conn := dialProxy(proxyServer)

			req := test_util.NewRequest("GET", "my_host.com", "/resource+9-9_9?query=123&query$2=345#page1..5", nil)
			conn.WriteRequest(req)

			res, _ := conn.ReadResponse()

			okCodes := []int{http.StatusOK, http.StatusFound}
			Expect(okCodes).Should(ContainElement(res.StatusCode))
		})
	})

	Context("when the route service is a CF app and hairpinning is enabled", func() {
		BeforeEach(func() {
			conf.RouteServicesHairpinning = true
		})
		Context("when registration message does not contain tls_port", func() {
			It("successfully looks up the route service and sends the request", func() {
				routeServiceHandler := func(conn *test_util.HttpConn) {
					defer GinkgoRecover()
					resp := test_util.NewResponse(http.StatusOK)
					req, _ := conn.ReadRequest()

					Expect(req.Host).ToNot(Equal("my_app.com"))
					metaHeader := req.Header.Get(routeservice.HeaderKeyMetadata)
					sigHeader := req.Header.Get(routeservice.HeaderKeySignature)

					crypto, err := secure.NewAesGCM([]byte(cryptoKey))
					Expect(err).ToNot(HaveOccurred())
					_, err = routeservice.SignatureContentsFromHeaders(sigHeader, metaHeader, crypto)
					Expect(err).ToNot(HaveOccurred())

					// X-CF-ApplicationID will only be set if the request was sent to internal cf app first time
					Expect(req.Header.Get("X-CF-ApplicationID")).To(Equal("my-route-service-app-id"))

					Expect(req.Header.Get("X-CF-Forwarded-Url")).To(Equal("https://my_app.com/"))
					conn.WriteResponse(resp)
				}

				rsListener := test_util.RegisterHandler(r, "route_service.com", routeServiceHandler, test_util.RegisterConfig{AppId: "my-route-service-app-id"})
				appListener := test_util.RegisterHandler(r, "my_app.com", func(conn *test_util.HttpConn) {
					conn.Close()
				}, test_util.RegisterConfig{RouteServiceUrl: "https://route_service.com"})

				defer func() {
					Expect(rsListener.Close()).ToNot(HaveErrored())
					Expect(appListener.Close()).ToNot(HaveErrored())
				}()

				fakeRouteServicesClient.RoundTripStub = func(r *http.Request) (*http.Response, error) {
					return &http.Response{
						Request:    r,
						Body:       ioutil.NopCloser(bytes.NewBufferString("{}")),
						StatusCode: http.StatusOK,
						Header:     make(map[string][]string),
					}, nil
				}

				conn := dialProxy(proxyServer)
				conn.WriteRequest(test_util.NewRequest("GET", "my_app.com", "", nil))

				res, _ := conn.ReadResponse()
				Expect(res.StatusCode).To(Equal(http.StatusOK))
			})
		})

		Context("when registration message contains tls_port", func() {
			var rsTLSCert tls.Certificate
			BeforeEach(func() {
				var err error
				certChain := test_util.CreateSignedCertWithRootCA(test_util.CertNames{CommonName: "route-service-san"})
				rsTLSCert, err = tls.X509KeyPair(certChain.CertPEM, certChain.PrivKeyPEM)
				Expect(err).NotTo(HaveOccurred())

				caCertPool = x509.NewCertPool()
				Expect(err).NotTo(HaveOccurred())
				caCertPool.AddCert(certChain.CACert)
			})

			It("successfully looks up the route service and sends the request", func() {
				routeServiceHandler := func(conn *test_util.HttpConn) {
					defer GinkgoRecover()
					resp := test_util.NewResponse(http.StatusOK)
					req, _ := conn.ReadRequest()

					Expect(req.Host).ToNot(Equal("my_app.com"))
					metaHeader := req.Header.Get(routeservice.HeaderKeyMetadata)
					sigHeader := req.Header.Get(routeservice.HeaderKeySignature)

					crypto, err := secure.NewAesGCM([]byte(cryptoKey))
					Expect(err).ToNot(HaveOccurred())
					_, err = routeservice.SignatureContentsFromHeaders(sigHeader, metaHeader, crypto)
					Expect(err).ToNot(HaveOccurred())

					// X-CF-ApplicationID will only be set if the request was sent to internal cf app first time
					Expect(req.Header.Get("X-CF-ApplicationID")).To(Equal("my-route-service-app-id"))

					Expect(req.Header.Get("X-CF-Forwarded-Url")).To(Equal("https://my_app.com/"))
					conn.WriteResponse(resp)
				}

				rsListener := test_util.RegisterHandler(r, "route_service.com", routeServiceHandler, test_util.RegisterConfig{
					ServerCertDomainSAN: "route-service-san", InstanceId: "rs-instance", AppId: "my-route-service-app-id",
					TLSConfig: &tls.Config{
						Certificates: []tls.Certificate{rsTLSCert},
					},
				})

				appListener := test_util.RegisterHandler(r, "my_app.com", func(conn *test_util.HttpConn) {
					conn.Close()
				}, test_util.RegisterConfig{RouteServiceUrl: "https://route_service.com"})
				defer func() {
					Expect(rsListener.Close()).ToNot(HaveErrored())
					Expect(appListener.Close()).ToNot(HaveErrored())
				}()

				fakeRouteServicesClient.RoundTripStub = func(r *http.Request) (*http.Response, error) {
					return &http.Response{
						Request:    r,
						Body:       ioutil.NopCloser(bytes.NewBufferString("{}")),
						StatusCode: http.StatusOK,
						Header:     make(map[string][]string),
					}, nil
				}

				conn := dialProxy(proxyServer)

				req := test_util.NewRequest("GET", "my_app.com", "", nil)
				conn.WriteRequest(req)

				res, _ := conn.ReadResponse()
				Expect(res.StatusCode).To(Equal(http.StatusOK))
			})
		})
	})
})
