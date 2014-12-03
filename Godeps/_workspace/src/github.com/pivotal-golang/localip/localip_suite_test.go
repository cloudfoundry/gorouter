package localip_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestLocalip(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Localip Suite")
}
