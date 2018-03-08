package router

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"time"
)

type CertType int

const (
	isCA CertType = iota
	isServer
	isClient
)

type RouteServicesServer struct {
	listener   net.Listener
	port       string
	rootCA     *x509.CertPool
	clientCert tls.Certificate
	serverCert tls.Certificate
}

func NewRouteServicesServer() *RouteServicesServer {
	caDER, caPriv := createCA()
	clientDER, clientPriv := createCertificate(caDER, caPriv, isClient)
	serverDER, serverPriv := createCertificate(caDER, caPriv, isServer)

	rootCertPool := x509.NewCertPool()

	caPEM := pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: caDER,
	})

	if ok := rootCertPool.AppendCertsFromPEM(caPEM); !ok {
		panic("could not append root cert")
	}

	clientCert, err := tls.X509KeyPair(clientDER, clientPriv)
	if err != nil {
		panic(err)
	}

	serverCert, err := tls.X509KeyPair(serverDER, serverPriv)
	if err != nil {
		panic(err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		panic(err)
	}

	return &RouteServicesServer{
		listener:   l,
		port:       port,
		rootCA:     rootCertPool,
		clientCert: clientCert,
		serverCert: serverCert,
	}
}

func (rs *RouteServicesServer) Serve(server *http.Server, errChan chan error) error {
	tlsConfig := &tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{rs.serverCert},
		ClientCAs:    rs.rootCA,
	}

	go func() {
		err := server.Serve(tls.NewListener(rs.listener, tlsConfig))
		errChan <- err
	}()

	return nil
}

func (rs *RouteServicesServer) Stop() {
	rs.listener.Close()
}

func (rs *RouteServicesServer) GetRoundTripper() RouteServiceRoundTripper {
	return RouteServiceRoundTripper{
		port: rs.port,
		transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{rs.clientCert},
				RootCAs:      rs.rootCA,
			},
		},
	}
}

type RouteServiceRoundTripper struct {
	port      string
	transport http.RoundTripper
}

func (rc RouteServiceRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "https"
	req.URL.Host = fmt.Sprintf("127.0.0.1:%s", rc.port)

	return rc.transport.RoundTrip(req)
}

func createCA() ([]byte, *ecdsa.PrivateKey) {
	caPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	tmpl := createCertTemplate(isCA)

	caDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &caPriv.PublicKey, caPriv)
	if err != nil {
		panic(err)
	}

	return caDER, caPriv
}

func createCertificate(caCert []byte, caPriv *ecdsa.PrivateKey, certType CertType) ([]byte, []byte) {
	certPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	rootCert, err := x509.ParseCertificate(caCert)
	if err != nil {
		panic(err)
	}

	certTemplate := createCertTemplate(certType)

	certDER, err := x509.CreateCertificate(rand.Reader, &certTemplate, rootCert, &certPriv.PublicKey, caPriv)
	if err != nil {
		panic(err)
	}

	privBytes, err := x509.MarshalECPrivateKey(certPriv)
	if err != nil {
		panic(err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type: "EC PRIVATE KEY", Bytes: privBytes,
	})

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: certDER,
	})

	return certPEM, keyPEM
}

func createCertTemplate(certType CertType) x509.Certificate {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		panic(err)
	}

	tmpl := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{Organization: []string{"Route Services"}},
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0), // valid for one year
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	switch certType {
	case isCA:
		tmpl.IsCA = true
		tmpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
	case isServer:
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	case isClient:
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}

	return tmpl
}
