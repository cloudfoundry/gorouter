package restart_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRestart(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Restart Suite")
}
