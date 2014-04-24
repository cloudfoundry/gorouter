package cmdtest_matchers

import (
	"fmt"

	"github.com/vito/cmdtest"
)

func Say(pattern string) *SayMatcher {
	return &SayMatcher{
		Pattern: pattern,
	}
}

type SayMatcher struct {
	Pattern string

	expectError error
}

func (m *SayMatcher) Match(out interface{}) (bool, error) {
	session, ok := out.(*cmdtest.Session)
	if !ok {
		return false, fmt.Errorf("Cannot expect output from %#v.", out)
	}

	err := session.ExpectOutput(m.Pattern)
	if err != nil {
		m.expectError = err
		return false, nil
	}

	return true, nil
}

func (m *SayMatcher) FailureMessage(actual interface{}) string {
	return m.expectError.Error()
}

func (m *SayMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected to not see %#v\n", m.Pattern)
}
