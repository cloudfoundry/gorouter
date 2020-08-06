package fails

type ClassifierGroup []Classifier

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
