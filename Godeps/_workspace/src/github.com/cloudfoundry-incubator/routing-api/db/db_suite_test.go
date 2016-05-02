package db_test

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/storerunner/etcdstorerunner"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

var etcdClient storeadapter.StoreAdapter
var etcdPort int
var etcdUrl string
var etcdRunner *etcdstorerunner.ETCDClusterRunner
var etcdVersion = "etcdserver\":\"2.1.1"
var routingAPIBinPath string

func TestDB(t *testing.T) {
	RegisterFailHandler(Fail)

	etcdPort = 4001 + GinkgoParallelNode()
	etcdUrl = fmt.Sprintf("http://127.0.0.1:%d", etcdPort)
	etcdRunner = etcdstorerunner.NewETCDClusterRunner(etcdPort, 1, nil)
	etcdRunner.Start()
	etcdClient = etcdRunner.Adapter(nil)

	RunSpecs(t, "DB Suite")

	etcdRunner.Stop()
}

var _ = BeforeSuite(func() {
	Expect(len(etcdRunner.NodeURLS())).Should(BeNumerically(">=", 1))

	etcdVersionUrl := etcdRunner.NodeURLS()[0] + "/version"
	resp, err := http.Get(etcdVersionUrl)
	Expect(err).ToNot(HaveOccurred())

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	Expect(err).ToNot(HaveOccurred())

	// response body: {"etcdserver":"2.1.1","etcdcluster":"2.1.0"}
	Expect(string(body)).To(ContainSubstring(etcdVersion))
})

var _ = BeforeEach(func() {
	etcdRunner.Reset()
})
