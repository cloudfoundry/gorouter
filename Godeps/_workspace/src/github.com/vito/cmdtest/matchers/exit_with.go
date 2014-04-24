package cmdtest_matchers

import (
	"fmt"
	"time"

	"github.com/vito/cmdtest"
)

func ExitWith(status int) *ExitWithMatcher {
	return &ExitWithMatcher{
		Status: status,
	}
}

type ExitWithMatcher struct {
	Status int

	actualStatus int
	waitError    error
}

func (m *ExitWithMatcher) Match(out interface{}) (bool, error) {
	session, ok := out.(*cmdtest.Session)
	if !ok {
		return false, fmt.Errorf("Cannot expect exit status from %#v.", out)
	}

	status, err := session.Wait(10 * time.Second)
	if err != nil {
		m.waitError = err
		return false, err
	}

	m.actualStatus = status

	return status == m.Status, nil
}

func (m *ExitWithMatcher) FailureMessage(actual interface{}) string {
	if m.waitError != nil {
		return m.waitError.Error()
	}

	return fmt.Sprintf("Exited with status %d, expected %d", m.actualStatus, m.Status)
}

func (m *ExitWithMatcher) NegatedFailureMessage(actual interface{}) string {
	if m.waitError != nil {
		return m.waitError.Error()
	}

	return fmt.Sprintf("Expected to not exit with %#v", m.Status)
}
