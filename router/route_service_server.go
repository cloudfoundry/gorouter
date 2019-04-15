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
	servers    []*http.Server
}

func NewRouteServicesServer() (*RouteServicesServer, error) {
	caCert, caPriv, err := createCA()
	if err != nil {
		return nil, fmt.Errorf("create ca: %s", err)
	}
	rootCertPool := x509.NewCertPool()
	rootCertPool.AddCert(caCert)

	clientCert, err := createCertificate(caCert, caPriv, isClient)
	if err != nil {
		return nil, fmt.Errorf("create client certificate: %s", err)
	}

	serverCert, err := createCertificate(caCert, caPriv, isServer)
	if err != nil {
		return nil, fmt.Errorf("create server certificate: %s", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting local listener: %s", err)
	}

	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("splitting host and port: %s", err)
	}

	return &RouteServicesServer{
		listener:   l,
		port:       port,
		rootCA:     rootCertPool,
		clientCert: clientCert,
		serverCert: serverCert,
	}, nil
}

func (rs *RouteServicesServer) Serve(handler http.Handler, errChan chan error) error {
	localServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler.ServeHTTP(w, r)
		}),
	}
	tlsConfig := &tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{rs.serverCert},
		ClientCAs:    rs.rootCA,
	}

	go func() {
		err := localServer.Serve(tls.NewListener(rs.listener, tlsConfig))
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

func createCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	caPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %s", err)
	}

	tmpl, err := createCertTemplate(isCA)
	if err != nil {
		return nil, nil, fmt.Errorf("create cert template: %s", err)
	}

	caDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &caPriv.PublicKey, caPriv)
	if err != nil {
		return nil, nil, fmt.Errorf("creating certificate: %s", err)
	}

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing ca cert: %s", err)
	}

	return caCert, caPriv, nil
}

func createCertificate(rootCert *x509.Certificate, caPriv *ecdsa.PrivateKey, certType CertType) (tls.Certificate, error) {
	certPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %s", err)
	}

	certTemplate, err := createCertTemplate(certType)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create cert template: %s", err)
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &certTemplate, rootCert, &certPriv.PublicKey, caPriv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("x509 create certificate: %s", err)
	}

	privBytes, err := x509.MarshalECPrivateKey(certPriv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal ec private key: %s", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type: "EC PRIVATE KEY", Bytes: privBytes,
	})

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: certDER,
	})

	x509KeyPair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("making x509 key pair: %s", err)
	}

	return x509KeyPair, nil
}

func createCertTemplate(certType CertType) (x509.Certificate, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return x509.Certificate{}, fmt.Errorf("random int: %s", err)
	}

	tmpl := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{Organization: []string{"Route Services"}},
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // valid for ten years: hopefully no one tries to run a single gorouter for that long without restarting!
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

	return tmpl, err
}
