package main_test

import (
	"fmt"
	"net/url"
	"os"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/cmd/routing-api/testrunner"
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	"testing"
	"time"
)

var etcdPort int
var etcdUrl string
var etcdRunner *etcdstorerunner.ETCDClusterRunner
var etcdAdapter storeadapter.StoreAdapter

var client routing_api.Client
var routingAPIBinPath string
var routingAPIAddress string
var routingAPIArgs testrunner.Args
var routingAPIRunner *ginkgomon.Runner
var routingAPIProcess ifrit.Process

func TestMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Main Suite")
}

var _ = SynchronizedBeforeSuite(
	func() []byte {
		routingAPIBin, err := gexec.Build("github.com/cloudfoundry-incubator/routing-api/cmd/routing-api", "-race")
		Expect(err).NotTo(HaveOccurred())
		return []byte(routingAPIBin)
	},
	func(routingAPIBin []byte) {
		routingAPIBinPath = string(routingAPIBin)
		SetDefaultEventuallyTimeout(15 * time.Second)
	},
)

var _ = SynchronizedAfterSuite(func() {
}, func() {
	gexec.CleanupBuildArtifacts()
})

var _ = BeforeEach(func() {
	etcdPort = 4001 + GinkgoParallelNode()
	etcdUrl = fmt.Sprintf("http://127.0.0.1:%d", etcdPort)
	etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1)
	etcdRunner.Start()

	etcdAdapter = etcdRunner.Adapter()
	port := 6900 + GinkgoParallelNode()
	routingAPIAddress = fmt.Sprintf("127.0.0.1:%d", port)

	routingAPIURL := &url.URL{
		Scheme: "http",
		Host:   routingAPIAddress,
	}

	client = routing_api.NewClient(routingAPIURL.String())
	workingDir, _ := os.Getwd()

	routingAPIArgs = testrunner.Args{
		Port:        port,
		ConfigPath:  workingDir + "/../../example_config/example.yml",
		EtcdCluster: etcdUrl,
		DevMode:     true,
	}
	routingAPIRunner = testrunner.New(routingAPIBinPath, routingAPIArgs)
})

var _ = AfterEach(func() {
	etcdAdapter.Disconnect()
	etcdRunner.Stop()
})
