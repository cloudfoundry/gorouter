package error_classifiers

import (
	"crypto/tls"
	"crypto/x509"
	"net"
)

type Classifier func(err error) bool

func AttemptedTLSWithNonTLSBackend(err error) bool {
	switch err.(type) {
	case tls.RecordHeaderError, *tls.RecordHeaderError:
		return true
	default:
		return false
	}
}

func Dial(err error) bool {
	ne, ok := err.(*net.OpError)
	return ok && ne.Op == "dial"
}

func ConnectionResetOnRead(err error) bool {
	ne, ok := err.(*net.OpError)
	return ok && ne.Op == "read" && ne.Err.Error() == "read: connection reset by peer"
}

func RemoteFailedCertCheck(err error) bool {
	ne, ok := err.(*net.OpError)
	return ok && ne.Op == "remote error" && ne.Err.Error() == "tls: bad certificate"
}

func RemoteHandshakeFailure(err error) bool {
	ne, ok := err.(*net.OpError)
	return ok && ne.Op == "remote error" && ne.Err.Error() == "tls: handshake failure"
}

func HostnameMismatch(err error) bool {
	switch err.(type) {
	case x509.HostnameError, *x509.HostnameError:
		return true
	default:
		return false
	}
}

func UntrustedCert(err error) bool {
	switch err.(type) {
	case x509.UnknownAuthorityError, *x509.UnknownAuthorityError:
		return true
	default:
		return false
	}
}
