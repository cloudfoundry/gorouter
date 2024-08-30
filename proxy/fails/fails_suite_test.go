package fails_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestFails(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Fails Suite")
}
