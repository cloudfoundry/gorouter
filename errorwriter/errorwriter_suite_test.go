package errorwriter_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestErrorwriter(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ErrorWriter Suite")
}
