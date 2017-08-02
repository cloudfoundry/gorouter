package handlers_test

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/handlers"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/urfave/negroni"
)

var _ = Describe("Clientcert", func() {
	var (
		nextReq           *http.Request
		n                 *negroni.Negroni
		clientCertHandler negroni.Handler
		nextHandler       http.HandlerFunc
	)

	Context("when ForwardedClientCert is set to sanitize_set", func() {
		BeforeEach(func() {
			nextReq = &http.Request{}
			clientCertHandler = handlers.NewClientCert(config.SANITIZE_SET)
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
			var (
				tlsConfig  *tls.Config
				httpClient *http.Client
			)
			BeforeEach(func() {
				httpClient = &http.Client{}
			})

			It("strips the xfcc headers from the request", func() {

				tlsCert1 := test_util.CreateCert("client_cert.com")

				servertlsConfig := &tls.Config{
					Certificates: []tls.Certificate{tlsCert1},
				}
				tlsConfig = &tls.Config{
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
			var (
				tlsConfig  *tls.Config
				httpClient *http.Client
			)
			BeforeEach(func() {
				httpClient = &http.Client{}
			})

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
				tlsConfig = &tls.Config{
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
			clientCertHandler = handlers.NewClientCert(config.FORWARD)
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
			var (
				tlsConfig  *tls.Config
				httpClient *http.Client
			)
			BeforeEach(func() {
				httpClient = &http.Client{}
			})

			It("strips the xfcc headers from the request", func() {

				tlsCert1 := test_util.CreateCert("client_cert.com")

				servertlsConfig := &tls.Config{
					Certificates: []tls.Certificate{tlsCert1},
				}
				tlsConfig = &tls.Config{
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
			var (
				tlsConfig  *tls.Config
				httpClient *http.Client
			)
			BeforeEach(func() {
				httpClient = &http.Client{}
			})

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
				tlsConfig = &tls.Config{
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

func sanitize(cert []byte) string {
	s := string(cert)
	r := strings.NewReplacer("-----BEGIN CERTIFICATE-----", "",
		"-----END CERTIFICATE-----", "",
		"\n", "")
	return r.Replace(s)
}
