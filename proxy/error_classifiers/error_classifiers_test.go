package error_classifiers_test

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/proxy/error_classifiers"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// If the golang standard library ever changes what kind
// of error it returns, one of these tests should catch that
var _ = Describe("ErrorClassifiers - enemy tests", func() {
	var (
		server, tlsServer      *httptest.Server
		testTransport          *http.Transport
		teapotHandler          http.Handler
		serverCert, clientCert test_util.CertChain
	)

	BeforeEach(func() {
		teapotHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})
		server = httptest.NewUnstartedServer(teapotHandler)
		tlsServer = httptest.NewUnstartedServer(teapotHandler)

		serverCert = test_util.CreateSignedCertWithRootCA("server")
		clientCert = test_util.CreateSignedCertWithRootCA("client")
		tlsServer.TLS = serverCert.AsTLSConfig()
		tlsServer.TLS.ClientCAs = x509.NewCertPool()
		tlsServer.TLS.ClientCAs.AddCert(clientCert.CACert)
		tlsServer.TLS.ClientAuth = tls.RequireAndVerifyClientCert

		testTransport = &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   1 * time.Second,
				KeepAlive: 1 * time.Second,
			}).DialContext,
			MaxIdleConns:          10,
			IdleConnTimeout:       2 * time.Second,
			TLSHandshakeTimeout:   2 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			TLSClientConfig:       clientCert.AsTLSConfig(),
		}
		testTransport.TLSClientConfig.RootCAs = x509.NewCertPool()
		testTransport.TLSClientConfig.RootCAs.AddCert(serverCert.CACert)
	})

	JustBeforeEach(func() {
		server.Start()
		tlsServer.StartTLS()
	})

	AfterEach(func() {
		server.Close()
		tlsServer.Close()
	})

	Describe("happy path mTLS", func() {
		It("works", func() {
			req, _ := http.NewRequest("GET", tlsServer.URL, nil)
			resp, err := testTransport.RoundTrip(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusTeapot))
			resp.Body.Close()
		})
	})

	Describe("AttemptedTLSWithNonTLSBackend", func() {
		It("matches when a TLS client attempts to connect to an http server", func() {
			url := strings.Replace(server.URL, "http", "https", -1)
			req, err := http.NewRequest("GET", url, nil)
			Expect(err).NotTo(HaveOccurred())

			_, err = testTransport.RoundTrip(req)
			Expect(err).To(HaveOccurred())
			Expect(error_classifiers.AttemptedTLSWithNonTLSBackend(err)).To(BeTrue())
		})

		It("does not match on other tls errors", func() {
			req, err := http.NewRequest("GET", tlsServer.URL, nil)
			Expect(err).NotTo(HaveOccurred())

			testTransport.TLSClientConfig.RootCAs = x509.NewCertPool() // create other error condition
			_, err = testTransport.RoundTrip(req)
			Expect(err).To(HaveOccurred())
			Expect(error_classifiers.AttemptedTLSWithNonTLSBackend(err)).To(BeFalse())
		})
	})

	Describe("Dial", func() {
		It("matches errors with TCP connections", func() {
			server.Close()
			req, _ := http.NewRequest("GET", server.URL, nil)

			_, err := testTransport.RoundTrip(req)
			Expect(err).To(HaveOccurred())
			Expect(error_classifiers.Dial(err)).To(BeTrue())
		})

		It("does not match TLS connection errors", func() {
			req, _ := http.NewRequest("GET", tlsServer.URL, nil)

			testTransport.TLSClientConfig.RootCAs = x509.NewCertPool() // create other error condition
			_, err := testTransport.RoundTrip(req)
			Expect(err).To(HaveOccurred())
			Expect(error_classifiers.Dial(err)).To(BeFalse())
		})
	})

	Describe("RemoteFailedTLSCertCheck", func() {
		Context("when the server expects client certs", func() {
			Context("when but the client doesn't provide client certs", func() {
				BeforeEach(func() {
					testTransport.TLSClientConfig.Certificates = []tls.Certificate{}
				})

				It("matches the error", func() {
					req, _ := http.NewRequest("GET", tlsServer.URL, nil)

					_, err := testTransport.RoundTrip(req)
					Expect(err).To(HaveOccurred())
					Expect(error_classifiers.RemoteFailedCertCheck(err)).To(BeTrue())
				})
			})

			Context("when the client-provided cert is not trusted by the server", func() {
				BeforeEach(func() {
					tlsServer.TLS.ClientCAs = x509.NewCertPool()
				})
				It("matches the error", func() {
					req, _ := http.NewRequest("GET", tlsServer.URL, nil)

					_, err := testTransport.RoundTrip(req)
					Expect(err).To(HaveOccurred())
					Expect(error_classifiers.RemoteFailedCertCheck(err)).To(BeTrue())
				})
			})

			Context("when another TLS error occurs", func() {
				BeforeEach(func() {
					tlsServer.TLS.CipherSuites = []uint16{tls.TLS_RSA_WITH_RC4_128_SHA}
				})
				It("does not match other tls errors", func() {
					req, _ := http.NewRequest("GET", tlsServer.URL, nil)

					_, err := testTransport.RoundTrip(req)
					Expect(err).To(HaveOccurred())
					Expect(error_classifiers.RemoteFailedCertCheck(err)).To(BeFalse())
				})
			})
		})
	})

	Describe("RemoteHandshakeFailure", func() {
		Context("whent the cipher suites aren't compatible", func() {
			BeforeEach(func() {
				tlsServer.TLS.CipherSuites = []uint16{tls.TLS_RSA_WITH_RC4_128_SHA}
			})

			It("matches", func() {
				req, _ := http.NewRequest("GET", tlsServer.URL, nil)

				_, err := testTransport.RoundTrip(req)
				Expect(err).To(HaveOccurred())
				Expect(error_classifiers.RemoteHandshakeFailure(err)).To(BeTrue())
			})
		})

		Context("when some other TLS error occurs", func() {
			BeforeEach(func() {
				tlsServer.TLS.MinVersion = tls.VersionTLS12
				testTransport.TLSClientConfig.MaxVersion = tls.VersionTLS11
			})

			It("matches", func() {
				req, _ := http.NewRequest("GET", tlsServer.URL, nil)

				_, err := testTransport.RoundTrip(req)
				Expect(err).To(HaveOccurred())
				Expect(error_classifiers.RemoteHandshakeFailure(err)).To(BeFalse())
			})
		})
	})
})
