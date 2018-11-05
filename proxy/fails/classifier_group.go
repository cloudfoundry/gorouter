package fails

type ClassifierGroup []Classifier

var RetriableClassifiers = ClassifierGroup{
	Dial,
	ConnectionResetOnRead,
	AttemptedTLSWithNonTLSBackend,
	HostnameMismatch,
	RemoteFailedCertCheck,
	RemoteHandshakeFailure,
	UntrustedCert,
}

// Classify returns true on errors that are in the classifier group
var FailableClassifiers = RetriableClassifiers

var PrunableClassifiers = ClassifierGroup{
	Dial,
	AttemptedTLSWithNonTLSBackend,
	HostnameMismatch,
	RemoteFailedCertCheck,
	RemoteHandshakeFailure,
	UntrustedCert,
}

// Classify returns true on errors that are retryable
func (cg ClassifierGroup) Classify(err error) bool {
	for _, classifier := range cg {
		if classifier.Classify(err) {
			return true
		}
	}
	return false
}
