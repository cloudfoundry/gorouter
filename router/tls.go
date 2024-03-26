package router

import (
	"crypto/tls"
	"net"

	"code.cloudfoundry.org/gorouter/logger"

	"github.com/uber-go/zap"
)

// A listener implements a network listener (net.Listener) for TLS connections.
// It is a modified version from the go standard library implementation found
// in [crypto/tls]. After accepting a new connection it starts a dedicated go
// routing to perform the TLS handshake in parallel. If an error is encountered
// it takes care of logging it together with the metadata available.
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

	go func(c *tls.Conn, log logger.Logger) {
		// TODO: Suppress the log from http.Server, see: golang/go#56183
		// TODO: Are deadlines already set on this connection? Probably not.
		err = c.Handshake()
		if err != nil {
			logTlsHandshakeErr(err, c, log)
			// The error is stored in the tls.Conn and will be re-used once the
			// [net/http.Server] tries to do its TLS handshake. While this is not optimal
			// since there is quite some setup done by the server it shouldn't be too
			// bad as the behaviour remains the same (e.g. we previously did the setup
			// as well and had to abort relatively late).
		}
	}(tlsC, l.logger)

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
		// Note: this will only trigger in cases where the initial verification of
		// of the certificate succeeded and the handshake failed at a later stage.
		// One example is custom validation via [tls.Config.VerifyPeerCertificate].
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
