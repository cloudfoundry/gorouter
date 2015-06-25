package db_test

import (
	"fmt"

	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DB Suite")
}

var etcdClient storeadapter.StoreAdapter
var etcdPort int
var etcdUrl string
var etcdRunner *etcdstorerunner.ETCDClusterRunner
var routingAPIBinPath string

var _ = BeforeEach(func() {
	etcdPort = 4001 + GinkgoParallelNode()
	etcdUrl = fmt.Sprintf("http://127.0.0.1:%d", etcdPort)
	etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1)
	etcdRunner.Start()

	etcdClient = etcdRunner.Adapter()
})

var _ = AfterEach(func() {
	etcdClient.Disconnect()
	etcdRunner.Stop()
})
