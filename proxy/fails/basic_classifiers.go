package fails

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
)

var IdempotentRequestEOFError = errors.New("EOF (via idempotent request)")

var IncompleteRequestError = errors.New("incomplete request")

var BackendOverloadedError = errors.New("backend overloaded")

var BackendOverloadedNotRetriableError = errors.New("backend overloaded (retry failed, request too large)")

var AttemptedTLSWithNonTLSBackend = ClassifierFunc(func(err error) bool {
	return errors.As(err, &tls.RecordHeaderError{})
})

var Dial = ClassifierFunc(func(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Op == "dial"
	}
	return false
})

var ContextCancelled = ClassifierFunc(func(err error) bool {
	return errors.Is(err, context.Canceled)
})

var ConnectionResetOnRead = ClassifierFunc(func(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Err.Error() == "read: connection reset by peer"
	}
	return false
})

var RemoteFailedCertCheck = ClassifierFunc(func(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Op == "remote error" && opErr.Err.Error() == "tls: bad certificate"
	}
	return false
})

var RemoteHandshakeTimeout = ClassifierFunc(func(err error) bool {
	return err != nil && strings.Contains(err.Error(), "net/http: TLS handshake timeout")
})

var ExpiredOrNotYetValidCertFailure = ClassifierFunc(func(err error) bool {
	var certErr x509.CertificateInvalidError
	if errors.As(err, &certErr) {
		return certErr.Reason == x509.Expired
	}
	return false
})

var RemoteHandshakeFailure = ClassifierFunc(func(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr != nil && opErr.Error() == "remote error: tls: handshake failure"
	}
	return false
})

var HostnameMismatch = ClassifierFunc(func(err error) bool {
	return errors.As(err, &x509.HostnameError{})
})

var UntrustedCert = ClassifierFunc(func(err error) bool {
	var tlsCertError *tls.CertificateVerificationError
	switch {
	case errors.As(err, &x509.UnknownAuthorityError{}), errors.As(err, &tlsCertError):
		return true
	default:
		return false
	}
})

var IdempotentRequestEOF = ClassifierFunc(func(err error) bool {
	return errors.Is(err, IdempotentRequestEOFError)
})

var IncompleteRequest = ClassifierFunc(func(err error) bool {
	return errors.Is(err, IncompleteRequestError)
})

var BackendOverloaded = ClassifierFunc(func(err error) bool {
	return errors.Is(err, BackendOverloadedError)
})
