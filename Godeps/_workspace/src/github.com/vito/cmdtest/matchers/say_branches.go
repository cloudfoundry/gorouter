package cmdtest_matchers

import (
	"fmt"

	"github.com/vito/cmdtest"
)

func SayBranches(branches ...cmdtest.ExpectBranch) *SayBranchesMatcher {
	return &SayBranchesMatcher{
		Branches: branches,
	}
}

type SayBranchesMatcher struct {
	Branches []cmdtest.ExpectBranch

	expectError error
}

func (m *SayBranchesMatcher) Match(out interface{}) (bool, error) {
	session, ok := out.(*cmdtest.Session)
	if !ok {
		return false, fmt.Errorf("Cannot expect output from %#v.", out)
	}

	err := session.ExpectOutputBranches(m.Branches...)
	if err != nil {
		m.expectError = err
		return false, nil
	}

	return true, nil
}

func (m *SayBranchesMatcher) FailureMessage(actual interface{}) string {
	return m.expectError.Error()
}

func (m *SayBranchesMatcher) NegatedFailureMessage(actual interface{}) string {
	return "Expected to not see any of the branches.\n"
}
