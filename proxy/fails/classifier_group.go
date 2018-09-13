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

var UnavailableClassifiers = ClassifierGroup{
	Dial,
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
