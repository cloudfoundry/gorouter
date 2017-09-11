package error_classifiers_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestErrorClassifiers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ErrorClassifiers Suite")
}
