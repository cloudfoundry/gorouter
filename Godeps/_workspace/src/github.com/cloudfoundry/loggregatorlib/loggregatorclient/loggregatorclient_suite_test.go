package loggregatorclient_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestLoggregatorclient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Loggregatorclient Suite")
}
