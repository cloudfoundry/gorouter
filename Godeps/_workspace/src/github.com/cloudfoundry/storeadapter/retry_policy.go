package storeadapter

import "time"

type ExponentialRetryPolicy struct{}

const maxRetryDelay = 16 * time.Second

func (ExponentialRetryPolicy) DelayFor(attempts uint) (time.Duration, bool) {
	// 20 attempts = around 5 minutes
	if attempts > 20 {
		return 0, false
	}

	exponentialDelay := (1 << (attempts - 1)) * time.Second
	if exponentialDelay > maxRetryDelay {
		return maxRetryDelay, true
	}

	return exponentialDelay, true
}
