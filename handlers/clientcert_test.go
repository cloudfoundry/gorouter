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
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/uber-go/zap"
	"github.com/urfave/negroni"
)

var _ = Describe("Clientcert", func() {
	var (
		stripCertNoTLS   = true
		noStripCertNoTLS = false
		stripCertTLS     = true
		noStripCertTLS   = false
		stripCertMTLS    = ""
		xfccSanitizeMTLS = "xfcc"
		certSanitizeMTLS = "cert"

		forceDeleteHeader      = func(req *http.Request) (bool, error) { return true, nil }
		dontForceDeleteHeader  = func(req *http.Request) (bool, error) { return false, nil }
		errorForceDeleteHeader = func(req *http.Request) (bool, error) { return false, errors.New("forceDelete error") }
		skipSanitization       = func(req *http.Request) bool { return true }
		dontSkipSanitization   = func(req *http.Request) bool { return false }
	)

	DescribeTable("Client Cert Error Handling", func(forceDeleteHeaderFunc func(*http.Request) (bool, error), skipSanitizationFunc func(*http.Request) bool, errorCase string) {
		logger := new(logger_fakes.FakeLogger)
		clientCertHandler := handlers.NewClientCert(skipSanitizationFunc, forceDeleteHeaderFunc, config.SANITIZE_SET, logger)

		nextHandlerWasCalled := false
		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { nextHandlerWasCalled = true })

		n := negroni.New()
		n.Use(clientCertHandler)
		n.UseHandlerFunc(nextHandler)

		req := test_util.NewRequest("GET", "xyz.com", "", nil)
		rw := httptest.NewRecorder()
		clientCertHandler.ServeHTTP(rw, req, nextHandler)

		message, zapFields := logger.ErrorArgsForCall(0)
		Expect(message).To(Equal("signature-validation-failed"))
		switch errorCase {
		case "sanitizeError":
			Expect(zapFields).To(ContainElement(zap.Error(errors.New("skipSanitization error"))))
		case "forceDeleteError":
			Expect(zapFields).To(ContainElement(zap.Error(errors.New("forceDelete error"))))
		default:
			Fail("Unexpected error case")
		}
		Expect(rw.Code).To(Equal(http.StatusBadRequest))
		Expect(rw.HeaderMap).NotTo(HaveKey("Connection"))
		Expect(rw.Body).To(ContainSubstring("Failed to validate Route Service Signature"))

		Expect(nextHandlerWasCalled).To(BeFalse())
	},
		Entry("forceDelete returns an error", errorForceDeleteHeader, skipSanitization, "forceDeleteError"),
	)

	DescribeTable("Client Cert Result", func(forceDeleteHeaderFunc func(*http.Request) (bool, error), skipSanitizationFunc func(*http.Request) bool, forwardedClientCert string, noTLSCertStrip bool, TLSCertStrip bool, mTLSCertStrip string) {
		logger := new(logger_fakes.FakeLogger)
		clientCertHandler := handlers.NewClientCert(skipSanitizationFunc, forceDeleteHeaderFunc, forwardedClientCert, logger)

		nextReq := &http.Request{}
		nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { nextReq = r })

		n := negroni.New()
		n.Use(clientCertHandler)
		n.UseHandlerFunc(nextHandler)

		By("when there is no tls connection", func() {
			req := test_util.NewRequest("GET", "xyz.com", "", nil)
			req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
			rw := httptest.NewRecorder()
			clientCertHandler.ServeHTTP(rw, req, nextHandler)

			if noTLSCertStrip {
				Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
			} else {
				Expect(nextReq.Header["X-Forwarded-Client-Cert"]).To(Equal([]string{
					"trusted-xfcc-header",
				}))
			}
		})

		By("when there is a tls connection with no client certs", func() {
			tlsCert1 := test_util.CreateCert("client_cert.com")
			servertlsConfig := &tls.Config{
				Certificates: []tls.Certificate{tlsCert1},
			}
			tlsConfig := &tls.Config{InsecureSkipVerify: true}

			server := httptest.NewUnstartedServer(n)
			server.TLS = servertlsConfig
			server.StartTLS()
			defer server.Close()

			transport := &http.Transport{TLSClientConfig: tlsConfig}
			client := &http.Client{Transport: transport}

			req, err := http.NewRequest("GET", server.URL, nil)
			Expect(err).NotTo(HaveOccurred())

			req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
			_, err = client.Do(req)
			Expect(err).ToNot(HaveOccurred())

			if TLSCertStrip {
				Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
			} else {
				Expect(nextReq.Header["X-Forwarded-Client-Cert"]).To(Equal([]string{
					"trusted-xfcc-header",
				}))
			}
		})
		By("when there is a mtls connection with client certs", func() {
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

			transport := &http.Transport{TLSClientConfig: tlsConfig}
			client := &http.Client{Transport: transport}

			req, err := http.NewRequest("GET", server.URL, nil)
			Expect(err).NotTo(HaveOccurred())

			req.Header.Add("X-Forwarded-Client-Cert", "trusted-xfcc-header")
			_, err = client.Do(req)
			Expect(err).ToNot(HaveOccurred())

			switch mTLSCertStrip {
			case "":
				Expect(nextReq.Header).NotTo(HaveKey("X-Forwarded-Client-Cert"))
			case "xfcc":
				Expect(nextReq.Header["X-Forwarded-Client-Cert"]).To(Equal([]string{"trusted-xfcc-header"}))
			case "cert":
				Expect(nextReq.Header["X-Forwarded-Client-Cert"]).To(ConsistOf(sanitize(certPEM)))
			default:
				Fail("Unexpected mTLSCertStrip case")
			}
		})
	},
		Entry("when forceDeleteHeader, skipSanitization, and config.SANITIZE_SET", forceDeleteHeader, skipSanitization, config.SANITIZE_SET, stripCertNoTLS, stripCertTLS, stripCertMTLS),
		Entry("when forceDeleteHeader, skipSanitization, and config.FORWARD", forceDeleteHeader, skipSanitization, config.FORWARD, stripCertNoTLS, stripCertTLS, stripCertMTLS),
		Entry("when forceDeleteHeader, skipSanitization, and config.ALWAYS_FORWARD", forceDeleteHeader, skipSanitization, config.ALWAYS_FORWARD, stripCertNoTLS, stripCertTLS, stripCertMTLS),
		Entry("when forceDeleteHeader, dontSkipSanitization, and config.SANITIZE_SET", forceDeleteHeader, dontSkipSanitization, config.SANITIZE_SET, stripCertNoTLS, stripCertTLS, stripCertMTLS),
		Entry("when forceDeleteHeader, dontSkipSanitization, and config.FORWARD", forceDeleteHeader, dontSkipSanitization, config.FORWARD, stripCertNoTLS, stripCertTLS, stripCertMTLS),
		Entry("when forceDeleteHeader, dontSkipSanitization, and config.ALWAYS_FORWARD", forceDeleteHeader, dontSkipSanitization, config.ALWAYS_FORWARD, stripCertNoTLS, stripCertTLS, stripCertMTLS),
		Entry("when dontForceDeleteHeader, skipSanitization, and config.SANITIZE_SET", dontForceDeleteHeader, skipSanitization, config.SANITIZE_SET, noStripCertNoTLS, noStripCertTLS, xfccSanitizeMTLS),
		Entry("when dontForceDeleteHeader, skipSanitization, and config.FORWARD", dontForceDeleteHeader, skipSanitization, config.FORWARD, noStripCertNoTLS, noStripCertTLS, xfccSanitizeMTLS),
		Entry("when dontForceDeleteHeader, skipSanitization, and config.ALWAYS_FORWARD", dontForceDeleteHeader, skipSanitization, config.ALWAYS_FORWARD, noStripCertNoTLS, noStripCertTLS, xfccSanitizeMTLS),
		Entry("when dontForceDeleteHeader, dontSkipSanitization, and config.SANITIZE_SET", dontForceDeleteHeader, dontSkipSanitization, config.SANITIZE_SET, stripCertNoTLS, stripCertTLS, certSanitizeMTLS),
		Entry("when dontForceDeleteHeader, dontSkipSanitization, and config.FORWARD", dontForceDeleteHeader, dontSkipSanitization, config.FORWARD, stripCertNoTLS, stripCertTLS, xfccSanitizeMTLS),
		Entry("when dontForceDeleteHeader, dontSkipSanitization, and config.ALWAYS_FORWARD", dontForceDeleteHeader, dontSkipSanitization, config.ALWAYS_FORWARD, noStripCertNoTLS, noStripCertTLS, xfccSanitizeMTLS),
	)
})

func sanitize(cert []byte) string {
	s := string(cert)
	r := strings.NewReplacer("-----BEGIN CERTIFICATE-----", "",
		"-----END CERTIFICATE-----", "",
		"\n", "")
	return r.Replace(s)
}
