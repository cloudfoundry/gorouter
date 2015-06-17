package router_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRouter(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Router Suite")
}

var _ = BeforeSuite(func() {
	SetDefaultEventuallyTimeout(15 * time.Second)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
	SetDefaultConsistentlyDuration(1 * time.Second)
	SetDefaultConsistentlyPollingInterval(10 * time.Millisecond)
})
