package main_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestLogparser(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Logparser Suite")
}
