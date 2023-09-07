package uuid_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"testing"
)

func TestUuid(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Uuid Suite")
}
