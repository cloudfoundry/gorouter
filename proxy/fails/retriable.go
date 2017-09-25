package fails

type Retriable struct {
	RetryOnAny []Classifier
}

var DefaultRetryOnAny = []Classifier{
	AttemptedTLSWithNonTLSBackend,
	Dial,
	ConnectionResetOnRead,
	RemoteFailedCertCheck,
	RemoteHandshakeFailure,
	HostnameMismatch,
	UntrustedCert,
}

func (rc *Retriable) Classify(err error) bool {
	for _, classifier := range rc.RetryOnAny {
		if classifier.Classify(err) {
			return true
		}
	}
	return false
}
