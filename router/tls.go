package router

import (
	"crypto/tls"
	"net"

	"code.cloudfoundry.org/gorouter/logger"

	"github.com/uber-go/zap"
)

// A listener implements a network listener (net.Listener) for TLS connections.
// It is a modified version from the go standard library implementation found
// in src/crypto/tls/tls.go. It performs the handshake upon accepting the
// connection and takes care of emitting a proper error log should the
// handshake fail.
type listener struct {
	net.Listener
	config *tls.Config
	logger logger.Logger
}

// Accept waits for and returns the next incoming TLS connection after
// performing the TLS handshake. The returned connection is of type *tls.Conn.
func (l *listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	tlsC := tls.Server(c, l.config)

	// TODO: Suppress the log from http.Server, see: golang/go#56183
	err = tlsC.Handshake()
	if err != nil {
		logTlsHandshakeErr(err, tlsC, l.logger)
		// We do not return the error as returning an error from accept causes the
		// http.Server to think that the listener went bad. This would terminate the
		// gorouter which we must prevent. The error is stored in the tls.Conn and
		// will be re-used once the http.Server tries to do its TLS handshake. While
		// this is not optimal since there is quite some setup done by http.Server it
		// shouldn't be too bad as the behaviour remains the same.
	}

	return tlsC, nil
}

// NewListener creates a Listener which accepts connections from an inner
// Listener and wraps each connection with [Server].
// The configuration config must be non-nil and must include
// at least one certificate or else set GetCertificate.
func NewListener(inner net.Listener, config *tls.Config, logger logger.Logger) net.Listener {
	l := new(listener)
	l.Listener = inner
	l.logger = logger
	l.config = config
	return l
}

// logTlsHandshakeErr is a helper to conditionally log as much information as
// possible from a failed TLS handshake.
func logTlsHandshakeErr(err error, c *tls.Conn, log logger.Logger) {
	s := c.ConnectionState()

	fields := []zap.Field{
		zap.Error(err),
		zap.String("client_ip", c.RemoteAddr().String()),
		zap.Bool("tls_resumed", s.DidResume),
	}

	if len(s.PeerCertificates) > 0 {
		fields = append(fields,
			zap.String("client_cert_subject", s.PeerCertificates[0].Subject.String()),
			zap.String("client_cert_issuer", s.PeerCertificates[0].Issuer.String()),
		)
	}

	if s.CipherSuite != 0 {
		fields = append(fields, zap.String("cipher_suite", tls.CipherSuiteName(s.CipherSuite)))
	}

	if s.Version != 0 {
		fields = append(fields, zap.String("tls_version", tls.VersionName(s.Version)))
	}

	if s.ServerName != "" {
		fields = append(fields, zap.String("sni", s.ServerName))
	}

	log.Error("tls: listener: handshake failed", fields...)
}
