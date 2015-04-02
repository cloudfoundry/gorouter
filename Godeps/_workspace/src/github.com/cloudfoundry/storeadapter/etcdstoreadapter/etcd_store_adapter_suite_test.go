package etcdstoreadapter_test

import (
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"

	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"

	"os"
	"os/signal"
	"testing"
)

var etcdRunner *etcdstorerunner.ETCDClusterRunner

func TestStoreAdapter(t *testing.T) {
	registerSignalHandler()
	RegisterFailHandler(Fail)

	etcdPort := 5000 + (config.GinkgoConfig.ParallelNode-1)*10
	etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1)

	etcdRunner.Start()

	RunSpecs(t, "ETCD Store Adapter Suite")

	stopStores()
}

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
