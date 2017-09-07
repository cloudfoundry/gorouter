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
	"fmt"
	"math/big"
	"net"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/config"
	"code.cloudfoundry.org/gorouter/registry"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/routing-api/models"
)

func RegisterAddr(reg *registry.RouteRegistry, path string, addr string, cfg RegisterConfig) {
	host, portStr, err := net.SplitHostPort(addr)
	Expect(err).NotTo(HaveOccurred())

	port, err := strconv.Atoi(portStr)
	Expect(err).NotTo(HaveOccurred())
	reg.Register(
		route.Uri(path),
		route.NewEndpoint(
			cfg.AppId,
			host, uint16(port),
			cfg.InstanceId,
			cfg.InstanceIndex,
			nil, -1,
			cfg.RouteServiceUrl,
			models.ModificationTag{},
			"", cfg.IsTLS,
		),
	)
}

type connHandler func(*HttpConn)

func RegisterHandler(reg *registry.RouteRegistry, path string, handler connHandler, cfg ...RegisterConfig) net.Listener {
	var (
		ln  net.Listener
		err error
	)
	var rcfg RegisterConfig
	if len(cfg) > 0 {
		rcfg = cfg[0]
	}
	if rcfg.IsTLS {
		Expect(rcfg.TLSCert).NotTo(BeNil())

		var config *tls.Config
		config = &tls.Config{}
		config.Certificates = append(config.Certificates, rcfg.TLSCert)

		ln, err = tls.Listen("tcp", "127.0.0.1:0", config)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	Expect(err).NotTo(HaveOccurred())

	go runBackendInstance(ln, handler)

	if rcfg.InstanceIndex == "" {
		rcfg.InstanceIndex = "2"
	}
	RegisterAddr(reg, path, ln.Addr().String(), rcfg)

	return ln
}

type RegisterConfig struct {
	RouteServiceUrl string
	InstanceId      string
	InstanceIndex   string
	AppId           string
	IsTLS           bool
	TLSCert         tls.Certificate
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

func SpecSSLConfig(statusPort, proxyPort, SSLPort uint16, natsPorts ...uint16) (*config.Config, *x509.CertPool) {
	c := generateConfig(statusPort, proxyPort, natsPorts...)

	c.EnableSSL = true

	potatoCertchain := CreateSignedCertWithRootCA("potato.com")
	potato2Certchain := CreateSignedCertWithRootCA("potato2.com")

	clientTrustedCertPool := x509.NewCertPool()
	clientTrustedCertPool.AppendCertsFromPEM(potatoCertchain.CACertPEM)
	clientTrustedCertPool.AppendCertsFromPEM(potato2Certchain.CACertPEM)

	c.TLSPEM = []config.TLSPem{
		config.TLSPem{
			CertChain:  string(potatoCertchain.CertPEM),
			PrivateKey: string(potatoCertchain.PrivKeyPEM),
		},
		config.TLSPem{
			CertChain:  string(potato2Certchain.CertPEM),
			PrivateKey: string(potato2Certchain.PrivKeyPEM),
		},
	}
	c.SSLPort = SSLPort
	c.CipherString = "ECDHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-GCM-SHA384"

	return c, clientTrustedCertPool
}

func generateConfig(statusPort, proxyPort uint16, natsPorts ...uint16) *config.Config {
	c := config.DefaultConfig()

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

	c.Status = config.StatusConfig{
		Port: statusPort,
		User: "user",
		Pass: "pass",
	}

	c.Nats = []config.NatsConfig{}
	for _, natsPort := range natsPorts {
		c.Nats = append(c.Nats, config.NatsConfig{
			Host: "localhost",
			Port: natsPort,
			User: "nats",
			Pass: "nats",
		})
	}

	c.Logging = config.LoggingConfig{
		Level:         "debug",
		MetronAddress: "localhost:3457",
		JobName:       "router_test_z1_0",
	}

	c.OAuth = config.OAuthConfig{
		TokenEndpoint:     "uaa.cf.service.internal",
		Port:              8443,
		SkipSSLValidation: true,
	}

	c.RouteServiceSecret = "kCvXxNMB0JO2vinxoru9Hg=="

	c.Tracing = config.Tracing{
		EnableZipkin: true,
	}

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

func CreateSignedCertWithRootCA(commonName string) CertChain {
	rootPrivateKey, rootCADER := CreateCertDER("theCA")
	// generate a random serial number (a real cert authority would have some logic behind this)
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	Expect(err).ToNot(HaveOccurred())

	subject := pkix.Name{Organization: []string{"xyz, Inc."}}
	if commonName != "" {
		subject.CommonName = commonName
	}

	certTemplate := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               subject,
		SignatureAlgorithm:    x509.SHA256WithRSA,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour), // valid for an hour
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
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
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IsCA:                  true,
	}

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).ToNot(HaveOccurred())
	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &privKey.PublicKey, privKey)
	Expect(err).ToNot(HaveOccurred())
	return privKey, certDER
}

func CreateSignedCertDER(cname string, parentCert x509.Certificate, parentKey *rsa.PrivateKey) (*rsa.PrivateKey, []byte) {
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
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IsCA:                  false,
	}

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).ToNot(HaveOccurred())
	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &parentCert, &privKey.PublicKey, parentKey)
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

func CreateECCert(cname string) tls.Certificate {
	privKeyPEM, certPEM := CreateECKeyPair(cname)
	tlsCert, err := tls.X509KeyPair(certPEM, privKeyPEM)
	Expect(err).ToNot(HaveOccurred())
	return tlsCert
}
