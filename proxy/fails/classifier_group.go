package fails

type ClassifierGroup []Classifier

var RetriableClassifiers = ClassifierGroup{
	AttemptedTLSWithNonTLSBackend,
	Dial,
	ConnectionResetOnRead,
	RemoteFailedCertCheck,
	RemoteHandshakeFailure,
	HostnameMismatch,
	UntrustedCert,
}

var FailableClassifiers = RetriableClassifiers

var PrunableClassifiers = ClassifierGroup{
	HostnameMismatch,
	AttemptedTLSWithNonTLSBackend,
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
