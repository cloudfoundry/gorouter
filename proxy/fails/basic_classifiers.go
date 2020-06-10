package fails

import (
	"crypto/tls"
	"crypto/x509"
	"net"

	"context"
)

var AttemptedTLSWithNonTLSBackend = ClassifierFunc(func(err error) bool {
	switch err.(type) {
	case tls.RecordHeaderError, *tls.RecordHeaderError:
		return true
	default:
		return false
	}
})

var Dial = ClassifierFunc(func(err error) bool {
	ne, ok := err.(*net.OpError)
	return ok && ne.Op == "dial"
})

var ContextCancelled = ClassifierFunc(func(err error) bool {
	return err == context.Canceled
})

var ConnectionResetOnRead = ClassifierFunc(func(err error) bool {
	ne, ok := err.(*net.OpError)
	return ok && ne.Op == "read" && ne.Err.Error() == "read: connection reset by peer"
})

var RemoteFailedCertCheck = ClassifierFunc(func(err error) bool {
	if ne, ok := err.(*net.OpError); ok {
		return ne.Op == "remote error" && ne.Err.Error() == "tls: bad certificate"
	}

	return err != nil && err.Error() == "readLoopPeekFailLocked: remote error: tls: bad certificate"
})

var ExpiredOrNotYetValidCertFailure = ClassifierFunc(func(err error) bool {
	switch x509err := err.(type) {
	case x509.CertificateInvalidError:
		return x509err.Reason == x509.Expired
	case *x509.CertificateInvalidError:
		return x509err.Reason == x509.Expired
	default:
		return false
	}
})

var RemoteHandshakeFailure = ClassifierFunc(func(err error) bool {
	ne, ok := err.(*net.OpError)
	return ok && ne.Op == "remote error" && ne.Err.Error() == "tls: handshake failure"
})

var HostnameMismatch = ClassifierFunc(func(err error) bool {
	switch err.(type) {
	case x509.HostnameError, *x509.HostnameError:
		return true
	default:
		return false
	}
})

var UntrustedCert = ClassifierFunc(func(err error) bool {
	switch err.(type) {
	case x509.UnknownAuthorityError, *x509.UnknownAuthorityError:
		return true
	default:
		return false
	}
})
