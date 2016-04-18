package header_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestHeader(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Header Suite")
}
