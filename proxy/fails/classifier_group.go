package fails

type ClassifierGroup []Classifier

// RetriableClassifiers include backend errors that are safe to retry
//
// Backend errors are only safe to retry if we can be certain that they have
// occurred before any http request data has been sent from gorouter to the
// backend application.
//
// Otherwise, there’s risk of a mutating non-idempotent request (e.g. send
// payment) being silently retried without the client knowing.
var RetriableClassifiers = ClassifierGroup{
	Dial,
	AttemptedTLSWithNonTLSBackend,
	HostnameMismatch,
	RemoteFailedCertCheck,
	RemoteHandshakeFailure,
	RemoteHandshakeTimeout,
	UntrustedCert,
	ExpiredOrNotYetValidCertFailure,
}

var FailableClassifiers = ClassifierGroup{
	RetriableClassifiers,
	ConnectionResetOnRead,
}

var PrunableClassifiers = RetriableClassifiers

// Classify returns true on errors that are retryable
func (cg ClassifierGroup) Classify(err error) bool {
	for _, classifier := range cg {
		if classifier.Classify(err) {
			return true
		}
	}
	return false
}
