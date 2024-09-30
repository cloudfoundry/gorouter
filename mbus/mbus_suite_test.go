package mbus_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMbus(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mbus Suite")
}
