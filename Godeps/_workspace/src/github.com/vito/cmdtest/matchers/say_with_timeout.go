package cmdtest_matchers

import (
	"fmt"
	"time"

	"github.com/vito/cmdtest"
)

func SayWithTimeout(pattern string, timeout time.Duration) *SayWithTimeoutMatcher {
	return &SayWithTimeoutMatcher{
		Pattern: pattern,
		Timeout: timeout,
	}
}

type SayWithTimeoutMatcher struct {
	Pattern string
	Timeout time.Duration

	expectError error
}

func (m *SayWithTimeoutMatcher) Match(out interface{}) (bool, error) {
	session, ok := out.(*cmdtest.Session)
	if !ok {
		return false, fmt.Errorf("Cannot expect output from %#v.", out)
	}

	err := session.ExpectOutputWithTimeout(m.Pattern, m.Timeout)
	if err != nil {
		m.expectError = err
		return false, nil
	}

	return true, nil
}

func (m *SayWithTimeoutMatcher) FailureMessage(actual interface{}) string {
	return m.expectError.Error()
}

func (m *SayWithTimeoutMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected to not see %#v\n", m.Pattern)
}
