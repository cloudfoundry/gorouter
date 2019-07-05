package router_test

import (
	"code.cloudfoundry.org/gorouter/test_util"
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
	test_util.RunSpecWithHoneyCombReporter(t, "Router Suite")
}

var _ = SynchronizedBeforeSuite(func() []byte {
	fakeEmitter := fake.NewFakeEventEmitter("fake")
	dropsonde.InitializeWithEmitter(fakeEmitter)
	return nil
}, func([]byte) {
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
	SetDefaultConsistentlyDuration(1 * time.Second)
	SetDefaultConsistentlyPollingInterval(10 * time.Millisecond)
})
