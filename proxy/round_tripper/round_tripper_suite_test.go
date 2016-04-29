package round_tripper_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRoundTripper(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RoundTripper Suite")
}
