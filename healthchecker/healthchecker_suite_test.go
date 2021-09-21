package main_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestHealthchecker(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Healthchecker Suite")
}
