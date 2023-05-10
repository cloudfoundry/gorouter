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
//
// IMPORTANT: to truly determine whether a request is retry-able the function
// round_tripper.isRetrieable must be used. It includes additional checks that
// allow requests to be retried more often than it is allowed by the
// classifiers.
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

// FailableClassifiers match all errors that should result in the endpoint
// being marked as failed and taken out of the available pool. These endpoints
// will be cleaned up automatically by the route-pruning in case they have
// become stale, therefore there is no need to prune those endpoints
// proactively.
var FailableClassifiers = ClassifierGroup{
	Dial,
	AttemptedTLSWithNonTLSBackend,
	HostnameMismatch,
	RemoteFailedCertCheck,
	RemoteHandshakeFailure,
	RemoteHandshakeTimeout,
	UntrustedCert,
	ExpiredOrNotYetValidCertFailure,
	ConnectionResetOnRead,
}

// PrunableClassifiers match all errors that should result in the endpoint
// being pruned. This applies only if the connection to the backend is using
// TLS since the route-integrity prevents routes from being pruned
// automatically if they are configured with TLS.
var PrunableClassifiers = ClassifierGroup{
	Dial,
	AttemptedTLSWithNonTLSBackend,
	HostnameMismatch,
	RemoteFailedCertCheck,
	RemoteHandshakeFailure,
	RemoteHandshakeTimeout,
	UntrustedCert,
	ExpiredOrNotYetValidCertFailure,
}

// Classify returns true on errors that match the at least one Classifier from
// the ClassifierGroup it is called on.
func (cg ClassifierGroup) Classify(err error) bool {
	for _, classifier := range cg {
		if classifier.Classify(err) {
			return true
		}
	}
	return false
}
