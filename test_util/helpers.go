package test_util

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
)

func RegisterAddr(reg *registry.RouteRegistry, path string, addr string, cfg RegisterConfig) {
	host, portStr, err := net.SplitHostPort(addr)
	Expect(err).NotTo(HaveOccurred())

	port, err := strconv.Atoi(portStr)
	Expect(err).NotTo(HaveOccurred())
	reg.Register(
		route.Uri(path),
		route.NewEndpoint(&route.EndpointOpts{
			AppId:                   cfg.AppId,
			Host:                    host,
			Protocol:                cfg.Protocol,
			Port:                    uint16(port),
			ServerCertDomainSAN:     cfg.ServerCertDomainSAN,
			PrivateInstanceIndex:    cfg.InstanceIndex,
			PrivateInstanceId:       cfg.InstanceId,
			StaleThresholdInSeconds: cfg.StaleThreshold,
			RouteServiceUrl:         cfg.RouteServiceUrl,
			UseTLS:                  cfg.TLSConfig != nil,
		}),
	)
}

type connHandler func(*HttpConn)

func RegisterConnHandler(reg *registry.RouteRegistry, path string, handler connHandler, cfg ...RegisterConfig) net.Listener {
	var rcfg RegisterConfig
	if len(cfg) > 0 {
		rcfg = cfg[0]
	}
	ln, err := startBackendListener(rcfg)
	Expect(err).NotTo(HaveOccurred())

	go runBackendInstance(ln, handler)

	RegisterAddr(reg, path, ln.Addr().String(), prepareConfig(rcfg))

	return ln
}

func RegisterHTTPHandler(reg *registry.RouteRegistry, path string, handler http.HandlerFunc, cfg ...RegisterConfig) net.Listener {
	var rcfg RegisterConfig
	if len(cfg) > 0 {
		rcfg = cfg[0]
	}
	ln, err := startBackendListener(rcfg)
	Expect(err).NotTo(HaveOccurred())

	server := http.Server{Handler: handler}
	go server.Serve(ln)

	RegisterAddr(reg, path, ln.Addr().String(), prepareConfig(rcfg))

	return ln
}

func startBackendListener(rcfg RegisterConfig) (net.Listener, error) {
	if rcfg.TLSConfig != nil && !rcfg.IgnoreTLSConfig {
		return tls.Listen("tcp", "127.0.0.1:0", rcfg.TLSConfig)
	}
	return net.Listen("tcp", "127.0.0.1:0")
}

func prepareConfig(rcfg RegisterConfig) RegisterConfig {
	if rcfg.InstanceIndex == "" {
		rcfg.InstanceIndex = "2"
	}
	if rcfg.StaleThreshold == 0 {
		rcfg.StaleThreshold = 120
	}
	return rcfg
}

type RegisterConfig struct {
	RouteServiceUrl     string
	ServerCertDomainSAN string
	InstanceId          string
	InstanceIndex       string
	AppId               string
	StaleThreshold      int
	TLSConfig           *tls.Config
	IgnoreTLSConfig     bool
	Protocol            string
}

func runBackendInstance(ln net.Listener, handler connHandler) {
	var tempDelay time.Duration // how long to sleep on accept failure
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				fmt.Printf("http: Accept error: %v; retrying in %v\n", err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			break
		}
		go func() {
			defer GinkgoRecover()
			handler(NewHttpConn(conn))
		}()
	}
}

func SpecConfig(statusPort, proxyPort uint16, natsPorts ...uint16) *config.Config {
	return generateConfig(statusPort, proxyPort, natsPorts...)
}

func SpecSSLConfig(statusPort, proxyPort, SSLPort uint16, natsPorts ...uint16) (*config.Config, *tls.Config) {
	c := generateConfig(statusPort, proxyPort, natsPorts...)

	c.EnableSSL = true

	rootCertChain := CreateSignedCertWithRootCA(CertNames{SANs: SubjectAltNames{DNS: "*.localhost.routing.cf-app.com", IP: c.Ip}})
	secondaryCertChain := CreateSignedCertWithRootCA(CertNames{CommonName: "potato2.com"})

	clientTrustedCertPool := x509.NewCertPool()
	clientTrustedCertPool.AppendCertsFromPEM(rootCertChain.CACertPEM)
	clientTrustedCertPool.AppendCertsFromPEM(secondaryCertChain.CACertPEM)

	c.TLSPEM = []config.TLSPem{
		{
			CertChain:  string(rootCertChain.CertPEM),
			PrivateKey: string(rootCertChain.PrivKeyPEM),
		},
		{
			CertChain:  string(secondaryCertChain.CertPEM),
			PrivateKey: string(secondaryCertChain.PrivKeyPEM),
		},
	}
	c.CACerts = []string{string(rootCertChain.CACertPEM)}
	c.SSLPort = SSLPort
	c.CipherString = "ECDHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-GCM-SHA384"
	c.ClientCertificateValidationString = "none"

	return c, &tls.Config{
		RootCAs: clientTrustedCertPool,
	}
}

const (
	TLSConfigFromCACerts       = 1
	TLSConfigFromClientCACerts = 2
	TLSConfigFromUnknownCA     = 3
)

func CustomSpecSSLConfig(onlyTrustClientCACerts bool, TLSClientConfigOption int, statusPort, proxyPort, SSLPort uint16, natsPorts ...uint16) (*config.Config, *tls.Config) {
	c := generateConfig(statusPort, proxyPort, natsPorts...)

	c.EnableSSL = true

	rootCertChain := CreateSignedCertWithRootCA(CertNames{SANs: SubjectAltNames{DNS: "*.localhost.routing.cf-app.com", IP: c.Ip}})
	secondaryCertChain := CreateSignedCertWithRootCA(CertNames{CommonName: "potato2.com"})

	clientCaCertChain := CreateSignedCertWithRootCA(CertNames{SANs: SubjectAltNames{DNS: "*.localhost.routing.cf-app.com", IP: c.Ip}})
	clientTrustedCertPool := x509.NewCertPool()
	clientTrustedCertPool.AppendCertsFromPEM(clientCaCertChain.CACertPEM)

	c.TLSPEM = []config.TLSPem{
		{
			CertChain:  string(rootCertChain.CertPEM),
			PrivateKey: string(rootCertChain.PrivKeyPEM),
		},
		{
			CertChain:  string(secondaryCertChain.CertPEM),
			PrivateKey: string(secondaryCertChain.PrivKeyPEM),
		},
	}
	c.CACerts = []string{string(rootCertChain.CACertPEM)}
	c.ClientCACerts = string(clientCaCertChain.CACertPEM)

	if onlyTrustClientCACerts == false {
		clientTrustedCertPool.AppendCertsFromPEM(rootCertChain.CACertPEM)
		clientTrustedCertPool.AppendCertsFromPEM(secondaryCertChain.CACertPEM)
		c.ClientCACerts += strings.Join(c.CACerts, "")
	}
	c.SSLPort = SSLPort
	c.CipherString = "ECDHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-GCM-SHA384"
	c.ClientCertificateValidationString = "require"

	var clientTLSConfig *tls.Config

	switch TLSClientConfigOption {
	case TLSConfigFromCACerts:
		clientTLSConfig = rootCertChain.AsTLSConfig()

	case TLSConfigFromClientCACerts:
		clientTLSConfig = clientCaCertChain.AsTLSConfig()

	case TLSConfigFromUnknownCA:
		unknownCertChain := CreateSignedCertWithRootCA(CertNames{CommonName: "neopets-is-gr8.com"})
		clientTLSConfig = unknownCertChain.AsTLSConfig()
	}

	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(rootCertChain.CACertPEM)
	clientTLSConfig.RootCAs = certPool
	return c, clientTLSConfig
}

func generateConfig(statusPort, proxyPort uint16, natsPorts ...uint16) *config.Config {
	c, err := config.DefaultConfig()
	Expect(err).ToNot(HaveOccurred())

	c.Port = proxyPort
	c.Index = 2
	c.TraceKey = "my_trace_key"

	// Hardcode the IP to localhost to avoid leaving the machine while running tests
	c.Ip = "127.0.0.1"

	c.StartResponseDelayInterval = 1 * time.Second
	c.PublishStartMessageInterval = 10 * time.Second
	c.PruneStaleDropletsInterval = 0
	c.DropletStaleThreshold = 10 * time.Second
	c.PublishActiveAppsInterval = 0
	c.Zone = "z1"

	c.EndpointTimeout = 500 * time.Millisecond
	c.WebsocketDialTimeout = c.EndpointDialTimeout

	c.Status = config.StatusConfig{
		Port: statusPort,
		User: "user",
		Pass: "pass",
	}

	natsHosts := make([]config.NatsHost, len(natsPorts))
	for i, natsPort := range natsPorts {
		natsHosts[i].Hostname = "localhost"
		natsHosts[i].Port = natsPort
	}
	c.Nats = config.NatsConfig{
		User:  "nats",
		Pass:  "nats",
		Hosts: natsHosts,
	}

	c.Logging.Level = "debug"
	c.Logging.MetronAddress = "localhost:3457"
	c.Logging.JobName = "router_test_z1_0"

	c.OAuth = config.OAuthConfig{
		TokenEndpoint:     "uaa.cf.service.internal",
		Port:              8443,
		SkipSSLValidation: true,
	}

	c.RouteServiceSecret = "kCvXxNMB0JO2vinxoru9Hg=="

	c.Tracing = config.Tracing{
		EnableZipkin: true,
	}

	c.Backends.MaxAttempts = 3
	c.RouteServiceConfig.MaxAttempts = 3

	return c
}

type CertChain struct {
	CertPEM, CACertPEM       []byte
	PrivKeyPEM, CAPrivKeyPEM []byte

	CACert    *x509.Certificate
	CAPrivKey *rsa.PrivateKey
}

func (cc *CertChain) AsTLSConfig() *tls.Config {
	cert, err := tls.X509KeyPair(cc.CertPEM, cc.PrivKeyPEM)
	Expect(err).ToNot(HaveOccurred())
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
}

func (cc *CertChain) WriteCACertToDir(dir string) string {
	file, err := ioutil.TempFile(dir, "certs")
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(file.Name(), cc.CACertPEM, 0644)
	Expect(err).ToNot(HaveOccurred())

	return file.Name()
}

type SubjectAltNames struct {
	DNS string
	IP  string
}

type CertNames struct {
	CommonName string
	SANs       SubjectAltNames
}

func CreateExpiredSignedCertWithRootCA(cert CertNames) CertChain {
	rootPrivateKey, rootCADER := CreateCertDER("theCA")
	// generate a random serial number (a real cert authority would have some logic behind this)
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	Expect(err).ToNot(HaveOccurred())

	subject := pkix.Name{Organization: []string{"xyz, Inc."}}
	subject.CommonName = cert.CommonName

	certTemplate := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               subject,
		SignatureAlgorithm:    x509.SHA256WithRSA,
		NotBefore:             time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2010, time.November, 10, 23, 0, 0, 0, time.UTC),
		BasicConstraintsValid: true,
	}
	if cert.SANs.IP != "" {
		certTemplate.IPAddresses = []net.IP{net.ParseIP(cert.SANs.IP)}
	}
	if cert.SANs.DNS != "" {
		certTemplate.DNSNames = []string{cert.SANs.DNS}
	}
	rootCert, err := x509.ParseCertificate(rootCADER)
	Expect(err).NotTo(HaveOccurred())

	ownKey, err := rsa.GenerateKey(rand.Reader, 1024)
	Expect(err).NotTo(HaveOccurred())

	certDER, err := x509.CreateCertificate(rand.Reader, &certTemplate, rootCert, &ownKey.PublicKey, rootPrivateKey)
	Expect(err).NotTo(HaveOccurred())
	ownKeyPEM, ownCertPEM := CreateKeyPairFromDER(certDER, ownKey)
	rootKeyPEM, rootCertPEM := CreateKeyPairFromDER(rootCADER, rootPrivateKey)
	return CertChain{
		CertPEM:      ownCertPEM,
		PrivKeyPEM:   ownKeyPEM,
		CACertPEM:    rootCertPEM,
		CAPrivKeyPEM: rootKeyPEM,
		CACert:       rootCert,
		CAPrivKey:    rootPrivateKey,
	}
}
func CreateSignedCertWithRootCA(cert CertNames) CertChain {
	rootPrivateKey, rootCADER := CreateCertDER("theCA")
	// generate a random serial number (a real cert authority would have some logic behind this)
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	Expect(err).ToNot(HaveOccurred())

	subject := pkix.Name{Organization: []string{"xyz, Inc."}}

	certTemplate := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               subject,
		SignatureAlgorithm:    x509.SHA256WithRSA,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour), // valid for an hour
		BasicConstraintsValid: true,
	}
	if cert.SANs.IP != "" {
		certTemplate.IPAddresses = []net.IP{net.ParseIP(cert.SANs.IP)}
	}

	if cert.SANs.DNS != "" {
		certTemplate.DNSNames = []string{cert.SANs.DNS}
	}
	rootCert, err := x509.ParseCertificate(rootCADER)
	Expect(err).NotTo(HaveOccurred())

	ownKey, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())

	certDER, err := x509.CreateCertificate(rand.Reader, &certTemplate, rootCert, &ownKey.PublicKey, rootPrivateKey)
	Expect(err).NotTo(HaveOccurred())
	ownKeyPEM, ownCertPEM := CreateKeyPairFromDER(certDER, ownKey)
	rootKeyPEM, rootCertPEM := CreateKeyPairFromDER(rootCADER, rootPrivateKey)
	return CertChain{
		CertPEM:      ownCertPEM,
		PrivKeyPEM:   ownKeyPEM,
		CACertPEM:    rootCertPEM,
		CAPrivKeyPEM: rootKeyPEM,
		CACert:       rootCert,
		CAPrivKey:    rootPrivateKey,
	}
}

func (c *CertChain) TLSCert() tls.Certificate {
	cert, _ := tls.X509KeyPair(c.CertPEM, c.PrivKeyPEM)
	return cert
}

func CreateCertDER(cname string) (*rsa.PrivateKey, []byte) {
	// generate a random serial number (a real cert authority would have some logic behind this)
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	Expect(err).ToNot(HaveOccurred())

	subject := pkix.Name{Organization: []string{"xyz, Inc."}}
	if cname != "" {
		subject.CommonName = cname
	}

	tmpl := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               subject,
		SignatureAlgorithm:    x509.SHA256WithRSA,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour), // valid for an hour
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{cname},
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IsCA:                  true,
	}

	privKey, err := rsa.GenerateKey(rand.Reader, 1024)
	Expect(err).ToNot(HaveOccurred())
	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &privKey.PublicKey, privKey)
	Expect(err).ToNot(HaveOccurred())
	return privKey, certDER
}

func CreateKeyPairFromDER(certDER []byte, privKey *rsa.PrivateKey) (keyPEM, certPEM []byte) {
	b := pem.Block{Type: "CERTIFICATE", Bytes: certDER}
	certPEM = pem.EncodeToMemory(&b)
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})

	return
}

func CreateKeyPair(cname string) (keyPEM, certPEM []byte) {
	privKey, certDER := CreateCertDER(cname)

	b := pem.Block{Type: "CERTIFICATE", Bytes: certDER}
	certPEM = pem.EncodeToMemory(&b)
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})

	return
}

func CreateECKeyPair(cname string) (keyPEM, certPEM []byte) {
	// generate a random serial number (a real cert authority would have some logic behind this)
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	Expect(err).ToNot(HaveOccurred())

	subject := pkix.Name{Organization: []string{"xyz, Inc."}}
	if cname != "" {
		subject.CommonName = cname
	}

	tmpl := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               subject,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour), // valid for an hour
		BasicConstraintsValid: true,
	}

	elliptic := elliptic.P256()
	privKey, err := ecdsa.GenerateKey(elliptic, rand.Reader)
	Expect(err).ToNot(HaveOccurred())

	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &privKey.PublicKey, privKey)
	Expect(err).ToNot(HaveOccurred())

	b := pem.Block{Type: "CERTIFICATE", Bytes: certDER}
	certPEM = pem.EncodeToMemory(&b)
	privBytes, err := x509.MarshalECPrivateKey(privKey)
	Expect(err).ToNot(HaveOccurred())

	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privBytes,
	})
	// the values for oid came from https://golang.org/src/crypto/x509/x509.go?s=54495:54612#L290
	ecdsaOid, err := asn1.Marshal(asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2})
	Expect(err).ToNot(HaveOccurred())
	paramPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PARAMETERS", Bytes: ecdsaOid})
	keyPEM = []byte(fmt.Sprintf("%s%s", paramPEM, keyPEM))
	return
}

func CreateCert(cname string) tls.Certificate {
	privKeyPEM, certPEM := CreateKeyPair(cname)
	tlsCert, err := tls.X509KeyPair(certPEM, privKeyPEM)
	Expect(err).ToNot(HaveOccurred())
	return tlsCert
}

type HangingReadCloser struct {
	mu        sync.Mutex
	readCalls int
}

func (h *HangingReadCloser) Read(p []byte) (n int, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.readCalls++

	if h.readCalls < 2 {
		p[0] = '!'
		return 1, nil
	}
	time.Sleep(1000 * time.Second)
	return 0, errors.New("hanging read closer ran out of time")
}

func (h *HangingReadCloser) Close() error { return nil }

type SlowReadCloser struct {
	mu            sync.Mutex
	readCalls     int
	SleepDuration time.Duration
}

func (h *SlowReadCloser) Read(p []byte) (n int, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.readCalls++

	if h.readCalls < 2 {
		p[0] = '!'
		return 1, nil
	}

	time.Sleep(h.SleepDuration)
	return 0, errors.New("slow read closer request has timed out")
}

func (h *SlowReadCloser) Close() error { return nil }

// CreateCertAndAddCA creates a signed cert with a root CA and adds the CA
// to the specified cert pool
func CreateCertAndAddCA(cn CertNames, cp *x509.CertPool) CertChain {
	certChain := CreateSignedCertWithRootCA(cn)
	cp.AddCert(certChain.CACert)
	return certChain
}
