package proxy_test

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backend TLS", func() {
	var registerConfig test_util.RegisterConfig
	BeforeEach(func() {
		privateInstanceId, _ := uuid.GenerateUUID()
		backendCertChain := test_util.CreateSignedCertWithRootCA(test_util.CertNames{CommonName: privateInstanceId})
		clientCertChain := test_util.CreateSignedCertWithRootCA(test_util.CertNames{CommonName: "gorouter"})

		var err error
		// Add backend CA cert to Gorouter CA pool
		caCertPool, err = x509.SystemCertPool()
		Expect(err).NotTo(HaveOccurred())
		caCertPool.AddCert(backendCertChain.CACert)

		// Add gorouter CA cert to backend app CA pool
		backendCACertPool := x509.NewCertPool()
		Expect(err).NotTo(HaveOccurred())
		backendCACertPool.AddCert(clientCertChain.CACert)

		backendTLSConfig := backendCertChain.AsTLSConfig()
		backendTLSConfig.ClientCAs = backendCACertPool

		conf.Backends.ClientAuthCertificate, err = tls.X509KeyPair(clientCertChain.CertPEM, clientCertChain.PrivKeyPEM)
		Expect(err).NotTo(HaveOccurred())

		registerConfig = test_util.RegisterConfig{
			TLSConfig:  backendTLSConfig,
			InstanceId: privateInstanceId,
			AppId:      "app-1",
		}
	})

	registerAppAndTest := func() *http.Response {
		ln := test_util.RegisterHandler(r, "test", func(conn *test_util.HttpConn) {
			req, err := http.ReadRequest(conn.Reader)
			if err != nil {
				conn.WriteResponse(test_util.NewResponse(http.StatusInternalServerError))
				return
			}
			err = req.Body.Close()
			Expect(err).ToNot(HaveOccurred())
			conn.WriteResponse(test_util.NewResponse(http.StatusOK))
		}, registerConfig)
		defer ln.Close()

		conn := dialProxy(proxyServer)

		conn.WriteLines([]string{
			"GET / HTTP/1.1",
			"Host: test",
		})

		resp, _ := conn.ReadResponse()
		return resp
	}

	Context("when the backend does not require a client certificate", func() {
		It("makes an mTLS connection with the backend", func() {
			resp := registerAppAndTest()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})
	Context("when the backend requires a client certificate", func() {
		BeforeEach(func() {
			registerConfig.TLSConfig.ClientAuth = tls.RequireAndVerifyClientCert
		})

		It("makes an mTLS connection with the backend", func() {
			resp := registerAppAndTest()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
		Context("when the gorouter presents certs that the backend does not trust", func() {
			BeforeEach(func() {
				registerConfig.TLSConfig.ClientCAs = x509.NewCertPool()
			})
			It("returns a HTTP 496 status code", func() {
				resp := registerAppAndTest()
				Expect(resp.StatusCode).To(Equal(496))
			})
		})
		Context("when the gorouter does not present certs", func() {
			BeforeEach(func() {
				conf.Backends.ClientAuthCertificate = tls.Certificate{}
			})
			It("returns a HTTP 496 status code", func() {
				resp := registerAppAndTest()
				Expect(resp.StatusCode).To(Equal(496))
			})
		})
	})

	Context("when the backend instance certificate is signed with an invalid CA", func() {
		BeforeEach(func() {
			var err error
			caCertPool, err = x509.SystemCertPool()
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns a HTTP 526 status code", func() {
			resp := registerAppAndTest()
			Expect(resp.StatusCode).To(Equal(526))
		})
	})

	Context("when the backend instance id does not match the common name on the backend's cert", func() {
		BeforeEach(func() {
			registerConfig.InstanceId = "foo-instance"
		})

		It("returns a HTTP 503 Service Unavailable error", func() {
			resp := registerAppAndTest()
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
		})
	})

	Context("when the backend registration does not include instance id", func() {
		BeforeEach(func() {
			registerConfig.InstanceId = ""
		})

		It("fails to validate (backends registering with a tls_port MUST provide a name that we can validate on their server certificate)", func() {
			resp := registerAppAndTest()
			Expect(resp.StatusCode).To(Equal(http.StatusServiceUnavailable))
		})
	})
	Context("when the backend is only listening for non TLS connections", func() {
		BeforeEach(func() {
			registerConfig.IgnoreTLSConfig = true
		})
		It("returns a HTTP 525 SSL Handshake error", func() {
			resp := registerAppAndTest()
			Expect(resp.StatusCode).To(Equal(525))
		})
	})
})
