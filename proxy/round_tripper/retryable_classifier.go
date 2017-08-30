package round_tripper

import "net"

//go:generate counterfeiter -o fakes/fake_retryable_classifier.go . RetryableClassifier
type RetryableClassifier interface {
	IsRetryable(err error) bool
}

type RoundTripperRetryableClassifier struct{}

func (rc RoundTripperRetryableClassifier) IsRetryable(err error) bool {
	ne, ok := err.(*net.OpError)
	if ok && (ne.Op == "dial" || (ne.Op == "read" && ne.Err.Error() == "read: connection reset by peer")) {
		return true
	}

	return false
}
