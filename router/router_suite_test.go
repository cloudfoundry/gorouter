package router_test

import (
	"log"
	"time"

	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/emitter/fake"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRouter(t *testing.T) {
	RegisterFailHandler(Fail)
	log.SetOutput(GinkgoWriter)
	RunSpecs(t, "Router Suite")
}

var _ = SynchronizedBeforeSuite(func() []byte {
	fakeEmitter := fake.NewFakeEventEmitter("fake")
	dropsonde.InitializeWithEmitter(fakeEmitter)
	return nil
}, func([]byte) {
	SetDefaultEventuallyTimeout(15 * time.Second)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
	SetDefaultConsistentlyDuration(1 * time.Second)
	SetDefaultConsistentlyPollingInterval(10 * time.Millisecond)
})
