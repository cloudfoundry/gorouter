package test_helpers_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestTest_helpers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TestHelpers Suite")
}
