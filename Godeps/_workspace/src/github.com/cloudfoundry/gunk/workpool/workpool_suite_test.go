package workpool_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestWorkpool(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Workpool Suite")
}
