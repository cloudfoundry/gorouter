package proxy_test

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"

	"code.cloudfoundry.org/gorouter/accesslog"
	"code.cloudfoundry.org/gorouter/common/secure"
	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/proxy"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util"

	"testing"
	"time"

	sharedfakes "code.cloudfoundry.org/gorouter/fakes"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/emitter/fake"
	fakelogsender "github.com/cloudfoundry/dropsonde/log_sender/fake"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

//go:generate counterfeiter -o ../fakes/round_tripper.go --fake-name RoundTripper net/http.RoundTripper

var (
	r                       *registry.RouteRegistry
	p                       http.Handler
	f                       *os.File
	fakeReporter            *fakes.FakeCombinedReporter
	conf                    *config.Config
	proxyServer             net.Listener
	al                      accesslog.AccessLogger
	accessLogFile           *test_util.FakeFile
	ls                      *fakelogsender.FakeLogSender
	crypto                  secure.Crypto
	testLogger              logger.Logger
	cryptoPrev              secure.Crypto
	caCertPool              *x509.CertPool
	recommendHttps          bool
	heartbeatOK             *int32
	fakeEmitter             *fake.FakeEventEmitter
	fakeRouteServicesClient *sharedfakes.RoundTripper
	skipSanitization        func(req *http.Request) bool
)

func TestProxy(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proxy Suite")
}

var _ = BeforeEach(func() {
	heartbeatOK = new(int32)
	atomic.StoreInt32(heartbeatOK, 1)
	testLogger = test_util.NewTestZapLogger("test")
	var err error

	crypto, err = secure.NewAesGCM([]byte("ABCDEFGHIJKLMNOP"))
	Expect(err).NotTo(HaveOccurred())

	cryptoPrev = nil
	conf, err = config.DefaultConfig()
	Expect(err).ToNot(HaveOccurred())
	conf.TraceKey = "my_trace_key"
	conf.EndpointTimeout = 1 * time.Second
	conf.EndpointDialTimeout = 50 * time.Millisecond
	fakeReporter = &fakes.FakeCombinedReporter{}
	skipSanitization = func(*http.Request) bool { return false }
})

var _ = JustBeforeEach(func() {
	var err error
	r = registry.NewRouteRegistry(testLogger, conf, new(fakes.FakeRouteRegistryReporter))

	fakeEmitter = fake.NewFakeEventEmitter("fake")
	dropsonde.InitializeWithEmitter(fakeEmitter)

	f, err = ioutil.TempFile("", "fakeFile")
	Expect(err).NotTo(HaveOccurred())
	conf.AccessLog.File = f.Name()
	ls = fakelogsender.NewFakeLogSender()
	al, err = accesslog.CreateRunningAccessLogger(testLogger, ls, conf)
	Expect(err).NotTo(HaveOccurred())
	go al.Run()

	conf.EnableSSL = true
	if len(conf.CipherSuites) == 0 {
		conf.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA}
	}

	tlsConfig := &tls.Config{
		CipherSuites:       conf.CipherSuites,
		InsecureSkipVerify: conf.SkipSSLValidation,
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{conf.Backends.ClientAuthCertificate},
	}

	routeServiceConfig := routeservice.NewRouteServiceConfig(
		testLogger,
		conf.RouteServiceEnabled,
		conf.RouteServicesHairpinning,
		conf.RouteServiceTimeout,
		crypto,
		cryptoPrev,
		recommendHttps,
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

	p = proxy.NewProxy(testLogger, al, conf, r, fakeReporter, routeServiceConfig, tlsConfig, tlsConfig, heartbeatOK, fakeRouteServicesClient)

	server := http.Server{Handler: p}
	go server.Serve(proxyServer)
})

var _ = AfterEach(func() {
	proxyServer.Close()
	al.Stop()
	caCertPool = nil
	os.Remove(f.Name())
})

func shouldEcho(input string, expected string) {
	ln := test_util.RegisterHandler(r, "encoding", func(x *test_util.HttpConn) {
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
