package fails

type ClassifierGroup []Classifier

var ErrorTypes = ClassifierGroup{
	Dial,
	AttemptedTLSWithNonTLSBackend,
	HostnameMismatch,
	RemoteFailedCertCheck,
	RemoteHandshakeFailure,
	UntrustedCert,
}

//Classify returns true on errors that are in the classifier group
func (cg ClassifierGroup) Classify(err error) bool {
	for _, classifier := range cg {
		if classifier.Classify(err) {
			return true
		}
	}
	return false
}
