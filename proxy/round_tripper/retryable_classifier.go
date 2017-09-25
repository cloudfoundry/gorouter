package round_tripper

import "code.cloudfoundry.org/gorouter/proxy/error_classifiers"

type Retriable struct {
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

func (rc *Retriable) Classify(err error) bool {
	for _, classifier := range rc.RetryOnAny {
		if classifier.Classify(err) {
			return true
		}
	}
	return false
}
