package fails_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"testing"
)

func TestFails(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Fails Suite")
}
