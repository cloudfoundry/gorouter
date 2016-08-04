package main_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"testing"
)

var gorouterPath string

func TestGorouter(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Gorouter Suite")
}

var _ = BeforeSuite(func() {
	path, err := gexec.Build("code.cloudfoundry.org/gorouter", "-race")
	Expect(err).ToNot(HaveOccurred())
	gorouterPath = path
	SetDefaultEventuallyTimeout(15 * time.Second)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
	SetDefaultConsistentlyDuration(1 * time.Second)
	SetDefaultConsistentlyPollingInterval(10 * time.Millisecond)
})

var _ = AfterSuite(func() {
	gexec.CleanupBuildArtifacts()
})
