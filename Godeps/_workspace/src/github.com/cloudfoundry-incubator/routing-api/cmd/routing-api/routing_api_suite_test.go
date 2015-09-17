package main_test

import (
	"fmt"
	"net/url"
	"os"
	"text/template"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/cmd/routing-api/testrunner"
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

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
var routingAPIPort uint16
var routingAPIIP string
var routingAPISystemDomain string

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

var _ = SynchronizedAfterSuite(func() {}, gexec.CleanupBuildArtifacts)

var _ = BeforeEach(func() {
	etcdPort = 4001 + GinkgoParallelNode()
	etcdUrl = fmt.Sprintf("http://127.0.0.1:%d", etcdPort)
	etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1)
	etcdRunner.Start()

	etcdAdapter = etcdRunner.Adapter()
	routingAPIPort = uint16(6900 + GinkgoParallelNode())
	routingAPIIP = "127.0.0.1"
	routingAPISystemDomain = "example.com"
	routingAPIAddress = fmt.Sprintf("%s:%d", routingAPIIP, routingAPIPort)

	routingAPIURL := &url.URL{
		Scheme: "http",
		Host:   routingAPIAddress,
	}

	client = routing_api.NewClient(routingAPIURL.String())

	routingAPIArgs = testrunner.Args{
		Port:         routingAPIPort,
		IP:           routingAPIIP,
		SystemDomain: routingAPISystemDomain,
		ConfigPath:   createConfig(),
		EtcdCluster:  etcdUrl,
		DevMode:      true,
	}
})

var _ = AfterEach(func() {
	etcdAdapter.Disconnect()
	etcdRunner.Reset()
	etcdRunner.Stop()
})

func createConfig() string {
	type statsdConfig struct {
		Port int
	}
	actualStatsdConfig := statsdConfig{Port: 8125 + GinkgoParallelNode()}
	workingDir, _ := os.Getwd()
	template, err := template.ParseFiles(workingDir + "/../../example_config/example_template.yml")
	Expect(err).NotTo(HaveOccurred())
	configFilePath := fmt.Sprintf("/tmp/example_%d.yml", GinkgoParallelNode())
	configFile, err := os.Create(configFilePath)
	Expect(err).NotTo(HaveOccurred())

	err = template.Execute(configFile, actualStatsdConfig)
	configFile.Close()
	Expect(err).NotTo(HaveOccurred())

	return configFilePath
}
