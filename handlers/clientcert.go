package handlers

import (
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"strings"

	"github.com/urfave/negroni"
)

const xfcc = "X-Forwarded-Client-Cert"

type clientCert struct{}

func NewClientCert() negroni.Handler {
	return &clientCert{}
}

func (c *clientCert) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	r.Header.Del(xfcc)
	if r.TLS != nil {
		sanitizeHeader(r)
	}
	next(rw, r)
}

func sanitizeHeader(r *http.Request) {
	// we only care about the first cert at this moment
	if len(r.TLS.PeerCertificates) > 0 {
		cert := r.TLS.PeerCertificates[0]
		b := pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}
		certPEM := pem.EncodeToMemory(&b)
		r.Header.Add(xfcc, sanitize(certPEM))
	}
}

func sanitize(cert []byte) string {
	s := string(cert)
	r := strings.NewReplacer("-----BEGIN CERTIFICATE-----", "",
		"-----END CERTIFICATE-----", "",
		"\n", "")
	s = r.Replace(s)
	return base64.StdEncoding.EncodeToString([]byte(s))
}
