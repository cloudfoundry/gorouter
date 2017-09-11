package round_tripper

import "code.cloudfoundry.org/gorouter/proxy/error_classifiers"

//go:generate counterfeiter -o fakes/fake_retryable_classifier.go . RetryableClassifier
type RetryableClassifier interface {
	IsRetryable(err error) bool
}

type RoundTripperRetryableClassifier struct {
	RetryOnAny []error_classifiers.Classifier
}

var DefaultRetryOnAny = []error_classifiers.Classifier{
	error_classifiers.AttemptedTLSWithNonTLSBackend,
	error_classifiers.Dial,
	error_classifiers.ConnectionResetOnRead,
	error_classifiers.RemoteFailedCertCheck,
	error_classifiers.RemoteHandshakeFailure,
	error_classifiers.HostnameMismatch,
	error_classifiers.UntrustedCert,
}

func (rc *RoundTripperRetryableClassifier) IsRetryable(err error) bool {
	for _, classifier := range rc.RetryOnAny {
		if classifier(err) {
			return true
		}
	}
	return false
}
