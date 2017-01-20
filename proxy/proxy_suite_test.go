package proxy_test

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"

	"code.cloudfoundry.org/gorouter/access_log"
	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/emitter/fake"

	"testing"
	"time"

	"code.cloudfoundry.org/gorouter/metrics/reporter/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	r              *registry.RouteRegistry
	p              proxy.Proxy
	fakeReporter   *fakes.FakeProxyReporter
	conf           *config.Config
	proxyServer    net.Listener
	accessLog      access_log.AccessLogger
	accessLogFile  *test_util.FakeFile
	crypto         secure.Crypto
	logger         lager.Logger
	cryptoPrev     secure.Crypto
	caCertPool     *x509.CertPool
	recommendHttps bool
	heartbeatOK    int32
)

func TestProxy(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proxy Suite")
}

var _ = BeforeEach(func() {
	logger = lagertest.NewTestLogger("test")
	var err error

	crypto, err = secure.NewAesGCM([]byte("ABCDEFGHIJKLMNOP"))
	Expect(err).NotTo(HaveOccurred())

	cryptoPrev = nil

	conf = config.DefaultConfig()
	conf.TraceKey = "my_trace_key"
	conf.EndpointTimeout = 500 * time.Millisecond
	fakeReporter = &fakes.FakeProxyReporter{}
})

var _ = JustBeforeEach(func() {
	var err error
	r = registry.NewRouteRegistry(logger, conf, new(fakes.FakeRouteRegistryReporter))

	fakeEmitter := fake.NewFakeEventEmitter("fake")
	dropsonde.InitializeWithEmitter(fakeEmitter)

	accessLogFile = new(test_util.FakeFile)
	accessLog = access_log.NewFileAndLoggregatorAccessLogger(logger, "", accessLogFile)
	go accessLog.Run()

	conf.EnableSSL = true
	conf.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA}

	tlsConfig := &tls.Config{
		CipherSuites:       conf.CipherSuites,
		InsecureSkipVerify: conf.SkipSSLValidation,
		RootCAs:            caCertPool,
	}
	heartbeatOK = 1

	routeServiceConfig := routeservice.NewRouteServiceConfig(
		logger,
		conf.RouteServiceEnabled,
		conf.RouteServiceTimeout,
		crypto,
		cryptoPrev,
		recommendHttps,
	)
	p = proxy.NewProxy(logger, accessLog, conf, r, fakeReporter, routeServiceConfig, tlsConfig, &heartbeatOK)

	proxyServer, err = net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())

	server := http.Server{Handler: p}
	go server.Serve(proxyServer)
})

var _ = AfterEach(func() {
	proxyServer.Close()
	accessLog.Stop()
	caCertPool = nil
})

func shouldEcho(input string, expected string) {
	ln := registerHandler(r, "encoding", func(x *test_util.HttpConn) {
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
