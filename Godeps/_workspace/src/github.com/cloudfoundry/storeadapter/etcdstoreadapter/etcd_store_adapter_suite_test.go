package etcdstoreadapter_test

import (
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"

	"os"
	"os/signal"
	"testing"
	"time"
)

var etcdRunner *etcdstorerunner.ETCDClusterRunner

func TestStoreAdapter(t *testing.T) {
	registerSignalHandler()
	RegisterFailHandler(Fail)

	SetDefaultEventuallyTimeout(5 * time.Second)

	RunSpecs(t, "ETCD Store Adapter Suite")
}

var _ = SynchronizedBeforeSuite(func() []byte {
	return nil
}, func(encodedBuiltArtifacts []byte) {
	etcdPort := 5000 + (config.GinkgoConfig.ParallelNode)*10
	etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1)
	etcdRunner.Start()
})

var _ = SynchronizedAfterSuite(func() {
	if etcdRunner != nil {
		etcdRunner.Stop()
	}
}, func() {
	gexec.CleanupBuildArtifacts()
})

var _ = BeforeEach(func() {
	etcdRunner.Reset()
})

func stopStores() {
	etcdRunner.Stop()
}

func registerSignalHandler() {
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, os.Kill)

		select {
		case <-c:
			stopStores()
			os.Exit(0)
		}
	}()
}
