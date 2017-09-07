package round_tripper

import (
	"crypto/tls"
	"crypto/x509"
	"net"
)

//go:generate counterfeiter -o fakes/fake_retryable_classifier.go . RetryableClassifier
type RetryableClassifier interface {
	IsRetryable(err error) bool
}

type RoundTripperRetryableClassifier struct{}

func isDialErr(ne *net.OpError) bool {
	return ne.Op == "dial"
}

func isConnectionResetError(ne *net.OpError) bool {
	return ne.Op == "read" && ne.Err.Error() == "read: connection reset by peer"
}

func isBadTLSCertError(ne *net.OpError) bool {
	return ne.Op == "remote error" && ne.Err.Error() == "tls: bad certificate"
}

func isHandshakeFailure(ne *net.OpError) bool {
	return ne.Op == "remote error" && ne.Err.Error() == "tls: handshake failure"
}

func (rc RoundTripperRetryableClassifier) IsRetryable(err error) bool {
	ne, ok := err.(*net.OpError)
	if ok && (isDialErr(ne) || isConnectionResetError(ne) || isBadTLSCertError(ne) || isHandshakeFailure(ne)) {
		return true
	}

	switch err.(type) {
	case *x509.HostnameError, *x509.UnknownAuthorityError, *tls.RecordHeaderError:
		return true
	default:
		return false
	}
}
