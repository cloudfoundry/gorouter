package cmdtest_matchers

import (
	"fmt"
	"time"

	"github.com/vito/cmdtest"
)

func ExitWithTimeout(status int, timeout time.Duration) *ExitWithTimeoutMatcher {
	return &ExitWithTimeoutMatcher{
		Status:  status,
		Timeout: timeout,
	}
}

type ExitWithTimeoutMatcher struct {
	Status  int
	Timeout time.Duration

	actualStatus int
	waitError    error
}

func (m *ExitWithTimeoutMatcher) Match(out interface{}) (bool, error) {
	session, ok := out.(*cmdtest.Session)
	if !ok {
		return false, fmt.Errorf("Cannot expect exit status from %#v.", out)
	}

	status, err := session.Wait(m.Timeout)
	if err != nil {
		m.waitError = err
		return false, nil
	}

	m.actualStatus = status

	return status == m.Status, nil
}

func (m *ExitWithTimeoutMatcher) FailureMessage(actual interface{}) string {
	if m.waitError != nil {
		return m.waitError.Error()
	}

	return fmt.Sprintf("Exited with status %d, expected %d", m.actualStatus, m.Status)
}

func (m *ExitWithTimeoutMatcher) NegatedFailureMessage(actual interface{}) string {
	if m.waitError != nil {
		return m.waitError.Error()
	}

	return fmt.Sprintf("Expected to not exit with %#v", m.Status)
}
