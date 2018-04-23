package handlers_test

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	logger_fakes "code.cloudfoundry.org/gorouter/logger/fakes"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

var _ = Describe("Clientcert", func() {
	var (
		nextReq           *http.Request
		n                 *negroni.Negroni
		clientCertHandler negroni.Handler
		nextHandler       http.HandlerFunc
		logger            *logger_fakes.FakeLogger
	)

	BeforeEach(func() {
		logger = new(logger_fakes.FakeLogger)
	})

	Context("when forceDeleteHeader is set to false", func() {
		forceDeleteHeader := func(req *http.Request) (bool, error) {
			return false, nil
		}

		Context("when ForwardedClientCert is set to always_forward", func() {
			BeforeEach(func() {
				nextReq = &http.Request{}
				clientCertHandler = handlers.NewClientCert(func(*http.Request) (bool, error) { return false, nil }, forceDeleteHeader, config.ALWAYS_FORWARD, logger)
				n = negroni.New()
				n.Use(clientCertHandler)
				nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					nextReq = r
				})
				n.UseHandlerFunc(nextHandler)
			})

			It("passes along any xfcc header that it recieves", func() {
				req := test_util.NewRequest("GET", "xyz.com", "", nil)
				req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
				req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")

				rw := httptest.NewRecorder()
				clientCertHandler.ServeHTTP(rw, req, nextHandler)
				Expect(nextReq.Header["X-Forwarded-Client-Cert"]).To(Equal([]string{
					"trusted-xfcc-header",
					"another-trusted-xfcc-header",
				}))
			})
		})

		Context("when skipSanitization is set to false", func() {
			skipSanitization := func(req *http.Request) (bool, error) {
				return false, nil
			}

			Context("when ForwardedClientCert is set to sanitize_set", func() {
				BeforeEach(func() {
					nextReq = &http.Request{}
					clientCertHandler = handlers.NewClientCert(skipSanitization, forceDeleteHeader, config.SANITIZE_SET, logger)
					n = negroni.New()
					n.Use(clientCertHandler)
					nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
						nextReq = r
					})
					n.UseHandlerFunc(nextHandler)
				})

				Context("when there is no tls connection", func() {
					var req *http.Request
					BeforeEach(func() {
						req = test_util.NewRequest("GET", "xyz.com", "", nil)
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")
						req.Header.Add("X-Forwarded-Client-Cert", "other-fake-cert")
					})

					It("strips any xfcc headers in the request", func() {
						rw := httptest.NewRecorder()
						clientCertHandler.ServeHTTP(rw, req, nextHandler)
						Expect(nextReq.Header["X-Forwarded-Client-Cert"]).To(BeEmpty())
					})
				})

				Context("when there is a tls connection with no client certs", func() {
					It("strips the xfcc headers from the request", func() {

						tlsCert1 := test_util.CreateCert("client_cert.com")

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert1},
						}
						tlsConfig := &tls.Config{
							InsecureSkipVerify: true,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert2")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						headerCerts := nextReq.Header["X-Forwarded-Client-Cert"]
						Expect(headerCerts).To(BeEmpty())
					})
				})

				Context("when there is a mtls connection with client certs", func() {
					It("sanitizes the xfcc headers from the request", func() {
						privKey, certDER := test_util.CreateCertDER("client_cert1.com")

						keyPEM, certPEM := test_util.CreateKeyPairFromDER(certDER, privKey)

						tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
						Expect(err).ToNot(HaveOccurred())

						x509Cert, err := x509.ParseCertificate(certDER)
						Expect(err).ToNot(HaveOccurred())

						certPool := x509.NewCertPool()
						certPool.AddCert(x509Cert)

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							ClientCAs:    certPool,
							ClientAuth:   tls.RequestClientCert,
						}
						tlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							RootCAs:      certPool,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert2")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						headerCerts := nextReq.Header["X-Forwarded-Client-Cert"]
						Expect(headerCerts).To(ConsistOf(sanitize(certPEM)))
					})
				})
			})

			Context("when ForwardedClientCert is set to forward", func() {
				BeforeEach(func() {
					nextReq = &http.Request{}
					clientCertHandler = handlers.NewClientCert(skipSanitization, forceDeleteHeader, config.FORWARD, logger)
					n = negroni.New()
					n.Use(clientCertHandler)
					nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
						nextReq = r
					})
					n.UseHandlerFunc(nextHandler)

				})
				Context("when there is no tls connection", func() {
					var req *http.Request
					BeforeEach(func() {
						req = test_util.NewRequest("GET", "xyz.com", "", nil)
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")
						req.Header.Add("X-Forwarded-Client-Cert", "other-fake-cert")
					})

					It("strips any xfcc headers in the request", func() {
						rw := httptest.NewRecorder()
						clientCertHandler.ServeHTTP(rw, req, nextHandler)
						Expect(nextReq.Header["X-Forwarded-Client-Cert"]).To(BeEmpty())
					})
				})

				Context("when there is a tls connection with no client certs", func() {
					It("strips the xfcc headers from the request", func() {

						tlsCert1 := test_util.CreateCert("client_cert.com")

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert1},
						}
						tlsConfig := &tls.Config{
							InsecureSkipVerify: true,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert2")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						headerCerts := nextReq.Header["X-Forwarded-Client-Cert"]
						Expect(headerCerts).To(BeEmpty())
					})
				})

				Context("when there is a mtls connection with client certs", func() {
					It("forwards the xfcc headers from the request", func() {
						privKey, certDER := test_util.CreateCertDER("client_cert1.com")

						keyPEM, certPEM := test_util.CreateKeyPairFromDER(certDER, privKey)

						tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
						Expect(err).ToNot(HaveOccurred())

						x509Cert, err := x509.ParseCertificate(certDER)
						Expect(err).ToNot(HaveOccurred())

						certPool := x509.NewCertPool()
						certPool.AddCert(x509Cert)

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							ClientCAs:    certPool,
							ClientAuth:   tls.RequestClientCert,
						}
						tlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							RootCAs:      certPool,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						headerCerts := nextReq.Header["X-Forwarded-Client-Cert"]
						Expect(headerCerts).To(ConsistOf("fake-cert"))
					})
				})
			})
		})

		Context("when skipSanitization is set to true", func() {
			skipSanitization := func(req *http.Request) (bool, error) {
				return true, nil
			}

			Context("when ForwardedClientCert is set to sanitize_set", func() {
				BeforeEach(func() {
					nextReq = &http.Request{}
					clientCertHandler = handlers.NewClientCert(skipSanitization, forceDeleteHeader, config.SANITIZE_SET, logger)
					n = negroni.New()
					n.Use(clientCertHandler)
					nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
						nextReq = r
					})
					n.UseHandlerFunc(nextHandler)
				})

				Context("when there is no tls connection", func() {
					var req *http.Request
					BeforeEach(func() {
						req = test_util.NewRequest("GET", "xyz.com", "", nil)
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
						req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")
					})

					It("does not strip any xfcc headers in the request", func() {
						rw := httptest.NewRecorder()
						clientCertHandler.ServeHTTP(rw, req, nextHandler)
						Expect(nextReq.Header["X-Forwarded-Client-Cert"]).To(Equal([]string{
							"trusted-xfcc-header",
							"another-trusted-xfcc-header",
						}))
					})
				})

				Context("when there is a tls connection with no client certs", func() {
					It("does not strip the xfcc headers from the request", func() {

						tlsCert1 := test_util.CreateCert("client_cert.com")

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert1},
						}
						tlsConfig := &tls.Config{
							InsecureSkipVerify: true,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
						req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						headerCerts := nextReq.Header["X-Forwarded-Client-Cert"]
						Expect(headerCerts).To(Equal([]string{
							"trusted-xfcc-header",
							"another-trusted-xfcc-header",
						}))
					})
				})

				Context("when there is a mtls connection with client certs", func() {
					It("does not sanitize the xfcc headers from the request", func() {
						privKey, certDER := test_util.CreateCertDER("client_cert1.com")

						keyPEM, certPEM := test_util.CreateKeyPairFromDER(certDER, privKey)

						tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
						Expect(err).ToNot(HaveOccurred())

						x509Cert, err := x509.ParseCertificate(certDER)
						Expect(err).ToNot(HaveOccurred())

						certPool := x509.NewCertPool()
						certPool.AddCert(x509Cert)

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							ClientCAs:    certPool,
							ClientAuth:   tls.RequestClientCert,
						}
						tlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							RootCAs:      certPool,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
						req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						headerCerts := nextReq.Header["X-Forwarded-Client-Cert"]
						Expect(headerCerts).To(Equal([]string{
							"trusted-xfcc-header",
							"another-trusted-xfcc-header",
						}))
					})
				})
			})

			Context("when ForwardedClientCert is set to forward", func() {
				BeforeEach(func() {
					nextReq = &http.Request{}
					clientCertHandler = handlers.NewClientCert(skipSanitization, forceDeleteHeader, config.FORWARD, logger)
					n = negroni.New()
					n.Use(clientCertHandler)
					nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
						nextReq = r
					})
					n.UseHandlerFunc(nextHandler)

				})
				Context("when there is no tls connection", func() {
					var req *http.Request
					BeforeEach(func() {
						req = test_util.NewRequest("GET", "xyz.com", "", nil)
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
						req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")
					})

					It("does not strip any xfcc headers in the request", func() {
						rw := httptest.NewRecorder()
						clientCertHandler.ServeHTTP(rw, req, nextHandler)
						Expect(nextReq.Header["X-Forwarded-Client-Cert"]).To(Equal([]string{
							"trusted-xfcc-header",
							"another-trusted-xfcc-header",
						}))
					})
				})

				Context("when there is a tls connection with no client certs", func() {
					It("does not strip the xfcc headers from the request", func() {

						tlsCert1 := test_util.CreateCert("client_cert.com")

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert1},
						}
						tlsConfig := &tls.Config{
							InsecureSkipVerify: true,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
						req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						headerCerts := nextReq.Header["X-Forwarded-Client-Cert"]
						Expect(headerCerts).To(Equal([]string{
							"trusted-xfcc-header",
							"another-trusted-xfcc-header",
						}))
					})
				})

				Context("when there is a mtls connection with client certs", func() {
					It("forwards the xfcc headers from the request", func() {
						privKey, certDER := test_util.CreateCertDER("client_cert1.com")

						keyPEM, certPEM := test_util.CreateKeyPairFromDER(certDER, privKey)

						tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
						Expect(err).ToNot(HaveOccurred())

						x509Cert, err := x509.ParseCertificate(certDER)
						Expect(err).ToNot(HaveOccurred())

						certPool := x509.NewCertPool()
						certPool.AddCert(x509Cert)

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							ClientCAs:    certPool,
							ClientAuth:   tls.RequestClientCert,
						}
						tlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							RootCAs:      certPool,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						headerCerts := nextReq.Header["X-Forwarded-Client-Cert"]
						Expect(headerCerts).To(ConsistOf("trusted-xfcc-header"))
					})
				})
			})
		})
	})

	Context("when forceDeleteHeader is set to true", func() {
		forceDeleteHeader := func(req *http.Request) (bool, error) {
			return true, nil
		}

		Context("when ForwardedClientCert is set to always_forward", func() {
			BeforeEach(func() {
				nextReq = &http.Request{}
				clientCertHandler = handlers.NewClientCert(func(*http.Request) (bool, error) { return false, nil }, forceDeleteHeader, config.ALWAYS_FORWARD, logger)
				n = negroni.New()
				n.Use(clientCertHandler)
				nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					nextReq = r
				})
				n.UseHandlerFunc(nextHandler)
			})

			It("strips any xfcc header that it recieves", func() {
				req := test_util.NewRequest("GET", "xyz.com", "", nil)
				req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
				req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")

				rw := httptest.NewRecorder()
				clientCertHandler.ServeHTTP(rw, req, nextHandler)
				Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
			})
		})

		Context("when skipSanitization is set to false", func() {
			skipSanitization := func(req *http.Request) (bool, error) {
				return false, nil
			}

			Context("when ForwardedClientCert is set to sanitize_set", func() {
				BeforeEach(func() {
					nextReq = &http.Request{}
					clientCertHandler = handlers.NewClientCert(skipSanitization, forceDeleteHeader, config.SANITIZE_SET, logger)
					n = negroni.New()
					n.Use(clientCertHandler)
					nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
						nextReq = r
					})
					n.UseHandlerFunc(nextHandler)
				})

				Context("when there is no tls connection", func() {
					var req *http.Request
					BeforeEach(func() {
						req = test_util.NewRequest("GET", "xyz.com", "", nil)
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")
						req.Header.Add("X-Forwarded-Client-Cert", "other-fake-cert")
					})

					It("strips any xfcc headers in the request", func() {
						rw := httptest.NewRecorder()
						clientCertHandler.ServeHTTP(rw, req, nextHandler)
						Expect(nextReq.Header["X-Forwarded-Client-Cert"]).To(BeEmpty())
					})
				})

				Context("when there is a tls connection with no client certs", func() {
					It("strips the xfcc headers from the request", func() {

						tlsCert1 := test_util.CreateCert("client_cert.com")

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert1},
						}
						tlsConfig := &tls.Config{
							InsecureSkipVerify: true,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert2")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						headerCerts := nextReq.Header["X-Forwarded-Client-Cert"]
						Expect(headerCerts).To(BeEmpty())
					})
				})

				Context("when there is a mtls connection with client certs", func() {
					It("strips the xfcc headers from the request", func() {
						privKey, certDER := test_util.CreateCertDER("client_cert1.com")

						keyPEM, certPEM := test_util.CreateKeyPairFromDER(certDER, privKey)

						tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
						Expect(err).ToNot(HaveOccurred())

						x509Cert, err := x509.ParseCertificate(certDER)
						Expect(err).ToNot(HaveOccurred())

						certPool := x509.NewCertPool()
						certPool.AddCert(x509Cert)

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							ClientCAs:    certPool,
							ClientAuth:   tls.RequestClientCert,
						}
						tlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							RootCAs:      certPool,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert2")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
					})
				})
			})

			Context("when ForwardedClientCert is set to forward", func() {
				BeforeEach(func() {
					nextReq = &http.Request{}
					clientCertHandler = handlers.NewClientCert(skipSanitization, forceDeleteHeader, config.FORWARD, logger)
					n = negroni.New()
					n.Use(clientCertHandler)
					nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
						nextReq = r
					})
					n.UseHandlerFunc(nextHandler)

				})
				Context("when there is no tls connection", func() {
					var req *http.Request
					BeforeEach(func() {
						req = test_util.NewRequest("GET", "xyz.com", "", nil)
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")
						req.Header.Add("X-Forwarded-Client-Cert", "other-fake-cert")
					})

					It("strips any xfcc headers in the request", func() {
						rw := httptest.NewRecorder()
						clientCertHandler.ServeHTTP(rw, req, nextHandler)
						Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
					})
				})

				Context("when there is a tls connection with no client certs", func() {
					It("strips the xfcc headers from the request", func() {

						tlsCert1 := test_util.CreateCert("client_cert.com")

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert1},
						}
						tlsConfig := &tls.Config{
							InsecureSkipVerify: true,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert2")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
					})
				})

				Context("when there is a mtls connection with client certs", func() {
					It("strips any xfcc header that it recieves", func() {
						privKey, certDER := test_util.CreateCertDER("client_cert1.com")

						keyPEM, certPEM := test_util.CreateKeyPairFromDER(certDER, privKey)

						tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
						Expect(err).ToNot(HaveOccurred())

						x509Cert, err := x509.ParseCertificate(certDER)
						Expect(err).ToNot(HaveOccurred())

						certPool := x509.NewCertPool()
						certPool.AddCert(x509Cert)

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							ClientCAs:    certPool,
							ClientAuth:   tls.RequestClientCert,
						}
						tlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							RootCAs:      certPool,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "fake-cert")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
					})
				})
			})
		})

		Context("when skipSanitization is set to true", func() {
			skipSanitization := func(req *http.Request) (bool, error) {
				return true, nil
			}

			Context("when ForwardedClientCert is set to sanitize_set", func() {
				BeforeEach(func() {
					nextReq = &http.Request{}
					clientCertHandler = handlers.NewClientCert(skipSanitization, forceDeleteHeader, config.SANITIZE_SET, logger)
					n = negroni.New()
					n.Use(clientCertHandler)
					nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
						nextReq = r
					})
					n.UseHandlerFunc(nextHandler)
				})

				Context("when there is no tls connection", func() {
					var req *http.Request
					BeforeEach(func() {
						req = test_util.NewRequest("GET", "xyz.com", "", nil)
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
						req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")
					})

					It("strips any xfcc header that it recieves", func() {
						rw := httptest.NewRecorder()
						clientCertHandler.ServeHTTP(rw, req, nextHandler)
						Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
					})
				})

				Context("when there is a tls connection with no client certs", func() {
					It("strips any xfcc header that it recieves", func() {

						tlsCert1 := test_util.CreateCert("client_cert.com")

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert1},
						}
						tlsConfig := &tls.Config{
							InsecureSkipVerify: true,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
						req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
					})
				})

				Context("when there is a mtls connection with client certs", func() {
					It("strips any xfcc header that it recieves", func() {
						privKey, certDER := test_util.CreateCertDER("client_cert1.com")

						keyPEM, certPEM := test_util.CreateKeyPairFromDER(certDER, privKey)

						tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
						Expect(err).ToNot(HaveOccurred())

						x509Cert, err := x509.ParseCertificate(certDER)
						Expect(err).ToNot(HaveOccurred())

						certPool := x509.NewCertPool()
						certPool.AddCert(x509Cert)

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							ClientCAs:    certPool,
							ClientAuth:   tls.RequestClientCert,
						}
						tlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							RootCAs:      certPool,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
						req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
					})
				})
			})

			Context("when ForwardedClientCert is set to forward", func() {
				BeforeEach(func() {
					nextReq = &http.Request{}
					clientCertHandler = handlers.NewClientCert(skipSanitization, forceDeleteHeader, config.FORWARD, logger)
					n = negroni.New()
					n.Use(clientCertHandler)
					nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
						nextReq = r
					})
					n.UseHandlerFunc(nextHandler)
				})

				Context("when there is no tls connection", func() {
					var req *http.Request
					BeforeEach(func() {
						req = test_util.NewRequest("GET", "xyz.com", "", nil)
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
						req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")
					})

					It("strips any xfcc header that it recieves", func() {
						rw := httptest.NewRecorder()
						clientCertHandler.ServeHTTP(rw, req, nextHandler)
						Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
					})
				})

				Context("when there is a tls connection with no client certs", func() {
					It("strips any xfcc header that it recieves", func() {

						tlsCert1 := test_util.CreateCert("client_cert.com")

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert1},
						}
						tlsConfig := &tls.Config{
							InsecureSkipVerify: true,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
						req.Header.Add("X-Forwarded-Client-Cert", "another-trusted-xfcc-header")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
					})
				})

				Context("when there is a mtls connection with client certs", func() {
					It("strips any xfcc header that it recieves", func() {
						privKey, certDER := test_util.CreateCertDER("client_cert1.com")

						keyPEM, certPEM := test_util.CreateKeyPairFromDER(certDER, privKey)

						tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
						Expect(err).ToNot(HaveOccurred())

						x509Cert, err := x509.ParseCertificate(certDER)
						Expect(err).ToNot(HaveOccurred())

						certPool := x509.NewCertPool()
						certPool.AddCert(x509Cert)

						servertlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							ClientCAs:    certPool,
							ClientAuth:   tls.RequestClientCert,
						}
						tlsConfig := &tls.Config{
							Certificates: []tls.Certificate{tlsCert},
							RootCAs:      certPool,
						}

						server := httptest.NewUnstartedServer(n)
						server.TLS = servertlsConfig

						server.StartTLS()
						defer server.Close()

						transport := &http.Transport{
							TLSClientConfig: tlsConfig,
						}

						req, err := http.NewRequest("GET", server.URL, nil)
						Expect(err).NotTo(HaveOccurred())

						// set original req x-for-cert
						req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")

						client := &http.Client{Transport: transport}
						_, err = client.Do(req)
						Expect(err).ToNot(HaveOccurred())

						Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
					})
				})
			})
		})
	})

	Context("when skipSanitization returns an error", func() {
		skipSanitization := func(req *http.Request) (bool, error) {
			return false, errors.New("skipSanitization error")
		}

		It("logs the error, writes the response, and returns", func() {
			forceDeleteHeader := func(req *http.Request) (bool, error) {
				return false, nil
			}

			clientCertHandler = handlers.NewClientCert(skipSanitization, forceDeleteHeader, config.SANITIZE_SET, logger)
			n = negroni.New()
			n.Use(clientCertHandler)
			nextHandlerWasCalled := false
			nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { nextHandlerWasCalled = true })
			n.UseHandlerFunc(nextHandler)

			req := test_util.NewRequest("GET", "xyz.com", "", nil)
			rw := httptest.NewRecorder()

			clientCertHandler.ServeHTTP(rw, req, nextHandler)

			message, zapFields := logger.ErrorArgsForCall(0)
			Expect(message).To(Equal("signature-validation-failed"))
			Expect(zapFields).To(ContainElement(zap.Error(errors.New("skipSanitization error"))))

			Expect(rw.Code).To(Equal(http.StatusBadRequest))
			Expect(rw.HeaderMap).NotTo(HaveKey("Connection"))
			Expect(rw.Body).To(ContainSubstring("Failed to validate Route Service Signature"))

			Expect(nextHandlerWasCalled).To(BeFalse())
		})
	})

	Context("when forceDelete returns an error", func() {
		forceDeleteHeader := func(req *http.Request) (bool, error) {
			return false, errors.New("forceDelete error")
		}

		It("logs the error, writes the response, and returns", func() {
			skipSanitization := func(req *http.Request) (bool, error) {
				return false, nil
			}

			clientCertHandler = handlers.NewClientCert(skipSanitization, forceDeleteHeader, config.SANITIZE_SET, logger)
			n = negroni.New()
			n.Use(clientCertHandler)
			nextHandlerWasCalled := false
			nextHandler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { nextHandlerWasCalled = true })
			n.UseHandlerFunc(nextHandler)

			req := test_util.NewRequest("GET", "xyz.com", "", nil)
			rw := httptest.NewRecorder()

			clientCertHandler.ServeHTTP(rw, req, nextHandler)

			message, zapFields := logger.ErrorArgsForCall(0)
			Expect(message).To(Equal("signature-validation-failed"))
			Expect(zapFields).To(ContainElement(zap.Error(errors.New("forceDelete error"))))

			Expect(rw.Code).To(Equal(http.StatusBadRequest))
			Expect(rw.HeaderMap).NotTo(HaveKey("Connection"))
			Expect(rw.Body).To(ContainSubstring("Failed to validate Route Service Signature"))

			Expect(nextHandlerWasCalled).To(BeFalse())
		})
	})
})

func sanitize(cert []byte) string {
	s := string(cert)
	r := strings.NewReplacer("-----BEGIN CERTIFICATE-----", "",
		"-----END CERTIFICATE-----", "",
		"\n", "")
	return r.Replace(s)
}
