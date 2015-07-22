package proxy_test

import (
	"crypto/tls"
	"net"
	"net/http"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/emitter/fake"
	"github.com/cloudfoundry/gorouter/access_log"
	"github.com/cloudfoundry/gorouter/common/secure"
	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/proxy"
	"github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/test_util"
	"github.com/cloudfoundry/yagnats/fakeyagnats"

	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	r             *registry.RouteRegistry
	p             proxy.Proxy
	conf          *config.Config
	proxyServer   net.Listener
	accessLog     access_log.AccessLogger
	accessLogFile *test_util.FakeFile
)

func TestProxy(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proxy Suite")
}

var _ = BeforeEach(func() {
	conf = config.DefaultConfig()
	conf.TraceKey = "my_trace_key"
	conf.EndpointTimeout = 500 * time.Millisecond
})

var _ = JustBeforeEach(func() {
	var err error
	mbus := fakeyagnats.Connect()
	r = registry.NewRouteRegistry(conf, mbus)

	fakeEmitter := fake.NewFakeEventEmitter("fake")
	dropsonde.InitializeWithEmitter(fakeEmitter)

	accessLogFile = new(test_util.FakeFile)
	accessLog = access_log.NewFileAndLoggregatorAccessLogger(accessLogFile, "")
	go accessLog.Run()

	conf.EnableSSL = true
	conf.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA}

	tlsConfig := &tls.Config{
		CipherSuites:       conf.CipherSuites,
		InsecureSkipVerify: conf.SSLSkipValidation,
	}

	crypto, err := secure.NewAesGCM([]byte("ABCDEFGHIJKLMNOP"))

	Expect(err).ToNot(HaveOccurred())

	p = proxy.NewProxy(proxy.ProxyArgs{
		EndpointTimeout:     conf.EndpointTimeout,
		Ip:                  conf.Ip,
		TraceKey:            conf.TraceKey,
		Registry:            r,
		Reporter:            nullVarz{},
		AccessLogger:        accessLog,
		SecureCookies:       conf.SecureCookies,
		TLSConfig:           tlsConfig,
		RouteServiceTimeout: conf.RouteServiceTimeout,
		Crypto:              crypto,
	})

	proxyServer, err = net.Listen("tcp", "127.0.0.1:0")
	Ω(err).NotTo(HaveOccurred())

	server := http.Server{Handler: p}
	go server.Serve(proxyServer)
})

var _ = AfterEach(func() {
	proxyServer.Close()
	accessLog.Stop()
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
