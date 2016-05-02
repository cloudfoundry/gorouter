package main_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/template"

	"github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/cmd/routing-api/testrunner"
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"

	"testing"
	"time"
)

var (
	etcdPort    int
	etcdUrl     string
	etcdRunner  *etcdstorerunner.ETCDClusterRunner
	etcdAdapter storeadapter.StoreAdapter

	client                 routing_api.Client
	routingAPIBinPath      string
	routingAPIAddress      string
	routingAPIArgs         testrunner.Args
	routingAPIPort         uint16
	routingAPIIP           string
	routingAPISystemDomain string
	oauthServer            *ghttp.Server
	oauthServerPort        string
)

var etcdVersion = "etcdserver\":\"2.1.1"

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

var _ = SynchronizedAfterSuite(func() {}, func() {
	gexec.CleanupBuildArtifacts()
})

var _ = BeforeEach(func() {
	etcdPort = 4001 + GinkgoParallelNode()
	etcdUrl = fmt.Sprintf("http://127.0.0.1:%d", etcdPort)
	etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1, nil)
	etcdRunner.Start()

	etcdVersionUrl := etcdRunner.NodeURLS()[0] + "/version"
	resp, err := http.Get(etcdVersionUrl)
	Expect(err).ToNot(HaveOccurred())

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	Expect(err).ToNot(HaveOccurred())

	// response body: {"etcdserver":"2.1.1","etcdcluster":"2.1.0"}
	Expect(string(body)).To(ContainSubstring(etcdVersion))

	etcdAdapter = etcdRunner.Adapter(nil)
	routingAPIPort = uint16(6900 + GinkgoParallelNode())
	routingAPIIP = "127.0.0.1"
	routingAPISystemDomain = "example.com"
	routingAPIAddress = fmt.Sprintf("%s:%d", routingAPIIP, routingAPIPort)

	routingAPIURL := &url.URL{
		Scheme: "http",
		Host:   routingAPIAddress,
	}

	client = routing_api.NewClient(routingAPIURL.String())

	oauthServer = ghttp.NewTLSServer()
	oauthServer.AllowUnhandledRequests = true
	oauthServer.UnhandledRequestStatusCode = http.StatusOK
	oauthServerPort = getServerPort(oauthServer.URL())

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
	oauthServer.Close()
})

func createConfig() string {
	type customConfig struct {
		Port    int
		UAAPort string
	}
	actualStatsdConfig := customConfig{Port: 8125 + GinkgoParallelNode(), UAAPort: oauthServerPort}
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

func getServerPort(url string) string {
	endpoints := strings.Split(url, ":")
	Expect(endpoints).To(HaveLen(3))
	return endpoints[2]
}
