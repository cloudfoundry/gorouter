package round_tripper_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRoundTripper(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RoundTripper Suite")
}
