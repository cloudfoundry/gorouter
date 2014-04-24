package cmdtest_matchers

import (
	"fmt"

	"github.com/vito/cmdtest"
)

func SayError(pattern string) *SayErrorMatcher {
	return &SayErrorMatcher{
		Pattern: pattern,
	}
}

type SayErrorMatcher struct {
	Pattern string

	expectError error
}

func (m *SayErrorMatcher) Match(out interface{}) (bool, error) {
	session, ok := out.(*cmdtest.Session)
	if !ok {
		return false, fmt.Errorf("Cannot expect output from %#v.", out)
	}

	err := session.ExpectError(m.Pattern)
	if err != nil {
		m.expectError = err
		return false, nil
	}

	return true, nil
}

func (m *SayErrorMatcher) FailureMessage(actual interface{}) string {
	return m.expectError.Error()
}

func (m *SayErrorMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected to not see %#v\n", m.Pattern)
}
