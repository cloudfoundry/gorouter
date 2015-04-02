package storenodematchers

import (
	"fmt"
	"reflect"

	"github.com/cloudfoundry/storeadapter"
	"github.com/onsi/gomega/format"
)

type moduloIndexMatcher struct {
	expected storeadapter.StoreNode
}

//MatchStoreNode matches store nodes without worrying about node.Index
//Use Equal if you want to ensure Indices line up.
func MatchStoreNode(expected storeadapter.StoreNode) *moduloIndexMatcher {
	return &moduloIndexMatcher{
		expected: expected,
	}
}

func (matcher *moduloIndexMatcher) Match(actual interface{}) (success bool, err error) {
	actualNode, ok := actual.(storeadapter.StoreNode)
	if !ok {
		return false, fmt.Errorf("Expected a store node.  Got:\n%s", format.Object(actual, 1))
	}

	matcher.expected.Index = actualNode.Index
	return reflect.DeepEqual(matcher.expected, actual), nil
}

func (matcher *moduloIndexMatcher) FailureMessage(actual interface{}) (message string) {
	actualNode, _ := actual.(storeadapter.StoreNode)
	matcher.expected.Index = actualNode.Index
	return format.Message(actual, "to be", matcher.expected)
}

func (matcher *moduloIndexMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	actualNode, _ := actual.(storeadapter.StoreNode)
	matcher.expected.Index = actualNode.Index
	return format.Message(actual, "not to be", matcher.expected)
}
