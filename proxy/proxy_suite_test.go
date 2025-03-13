package proxy_test

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/emitter/fake"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/accesslog"
	fakelogsender "code.cloudfoundry.org/gorouter/accesslog/schema/fakes"
	"code.cloudfoundry.org/gorouter/common/health"
	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/errorwriter"
	sharedfakes "code.cloudfoundry.org/gorouter/fakes"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"
)

//go:generate counterfeiter -o ../fakes/round_tripper.go --fake-name RoundTripper net/http.RoundTripper

var (
	r                         *registry.RouteRegistry
	p                         http.Handler
	f                         *os.File
	fakeReporter              *fakes.FakeProxyReporter
	conf                      *config.Config
	proxyServer               net.Listener
	al                        accesslog.AccessLogger
	ls                        *fakelogsender.FakeLogSender
	crypto                    secure.Crypto
	logger                    *test_util.TestLogger
	cryptoPrev                secure.Crypto
	caCertPool                *x509.CertPool
	recommendHTTPS            bool
	strictSignatureValidation bool
	healthStatus              *health.Health
	fakeEmitter               *fake.FakeEventEmitter
	fakeRouteServicesClient   *sharedfakes.RoundTripper
	skipSanitization          func(req *http.Request) bool
	ew                        = errorwriter.NewPlaintextErrorWriter()
)

func TestProxy(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proxy Suite")
}

var _ = BeforeEach(func() {
	healthStatus = &health.Health{}
	healthStatus.SetHealth(health.Healthy)
	logger = test_util.NewTestLogger("test")
	var err error

	crypto, err = secure.NewAesGCM([]byte("ABCDEFGHIJKLMNOP"))
	Expect(err).NotTo(HaveOccurred())

	cryptoPrev = nil
	conf, err = config.DefaultConfig()
	Expect(err).ToNot(HaveOccurred())
	conf.TraceKey = "my_trace_key"
	conf.TLSHandshakeTimeout = 900 * time.Millisecond
	conf.EndpointTimeout = 1 * time.Second
	conf.EndpointDialTimeout = 50 * time.Millisecond
	conf.WebsocketDialTimeout = 50 * time.Millisecond
	conf.EnableHTTP2 = false
	conf.Backends.MaxAttempts = 3
	conf.RouteServiceConfig.MaxAttempts = 3
	conf.DisableKeepAlives = false
	fakeReporter = &fakes.FakeProxyReporter{}
	strictSignatureValidation = false
	skipSanitization = func(*http.Request) bool { return false }
})

var _ = JustBeforeEach(func() {
	var err error
	r = registry.NewRouteRegistry(logger.Logger, conf, new(fakes.FakeRouteRegistryReporter))

	fakeEmitter = fake.NewFakeEventEmitter("fake")
	dropsonde.InitializeWithEmitter(fakeEmitter)

	f, err = os.CreateTemp("", "fakeFile")
	Expect(err).NotTo(HaveOccurred())
	conf.AccessLog.File = f.Name()
	ls = &fakelogsender.FakeLogSender{}
	al, err = accesslog.CreateRunningAccessLogger(logger.Logger, ls, conf)
	Expect(err).NotTo(HaveOccurred())
	go al.Run()

	conf.EnableSSL = true
	if len(conf.CipherSuites) == 0 {
		conf.CipherSuites = []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA}
	}

	tlsConfig := &tls.Config{
		CipherSuites:       conf.CipherSuites,
		InsecureSkipVerify: conf.SkipSSLValidation,
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{conf.Backends.ClientAuthCertificate},
	}

	routeServiceConfig := routeservice.NewRouteServiceConfig(
		logger.Logger,
		conf.RouteServiceEnabled,
		conf.RouteServicesHairpinning,
		conf.RouteServicesHairpinningAllowlist,
		conf.RouteServiceTimeout,
		crypto,
		cryptoPrev,
		recommendHTTPS,
		strictSignatureValidation,
	)

	proxyServer, err = net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())

	serverAddr := proxyServer.Addr().String()
	_, port, err := net.SplitHostPort(serverAddr)
	Expect(err).ToNot(HaveOccurred())
	intPort, err := strconv.Atoi(port)
	Expect(err).ToNot(HaveOccurred())
	conf.Port = uint16(intPort)

	fakeRouteServicesClient = &sharedfakes.RoundTripper{}

	p = proxy.NewProxy(logger.Logger, al, ew, conf, r, fakeReporter, routeServiceConfig, tlsConfig, tlsConfig, healthStatus, fakeRouteServicesClient)

	if conf.EnableHTTP2 {
		server := http.Server{Handler: p}
		tlsConfig.NextProtos = []string{"h2", "http/1.1"}
		tlsListener := tls.NewListener(proxyServer, tlsConfig)
		go server.Serve(tlsListener)
	} else {
		server := http.Server{Handler: p}
		go server.Serve(proxyServer)
	}
})

var _ = AfterEach(func() {
	proxyServer.Close()
	al.Stop()
	caCertPool = nil
	os.Remove(f.Name())
})

func shouldEcho(input string, expected string) {
	ln := test_util.RegisterConnHandler(r, "encoding", func(x *test_util.HttpConn) {
		x.CheckLine("GET " + expected + " HTTP/1.1")
		resp := test_util.NewResponse(http.StatusOK)
		x.WriteResponse(resp)
		x.Close()
	})
	defer ln.Close()

	x := dialProxy(proxyServer)

	req := test_util.NewRequest("GET", "encoding", input, nil)
	x.WriteRequest(req)
	resp, _ := x.ReadResponse()

	Expect(resp.StatusCode).To(Equal(http.StatusOK))
}
