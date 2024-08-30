package varz_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestVarz(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Varz Suite")
}
