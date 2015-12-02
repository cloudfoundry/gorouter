package etcdstorerunner

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cloudfoundry/gunk/workpool"
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/etcdstoreadapter"
	etcdclient "github.com/coreos/go-etcd/etcd"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/clock"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
)

type SSLConfig struct {
	CertFile string
	KeyFile  string
	CAFile   string
}

type ETCDClusterRunner struct {
	startingPort  int
	numNodes      int
	etcdProcesses []ifrit.Process
	running       bool
	client        *etcdclient.Client
	serverSSL     *SSLConfig

	mutex *sync.RWMutex
}

func NewETCDClusterRunner(startingPort int, numNodes int, serverSSL *SSLConfig) *ETCDClusterRunner {
	return &ETCDClusterRunner{
		startingPort: startingPort,
		numNodes:     numNodes,
		serverSSL:    serverSSL,

		mutex: &sync.RWMutex{},
	}
}

func (etcd *ETCDClusterRunner) Client() *etcdclient.Client {
	return etcd.client
}

func (etcd *ETCDClusterRunner) Start() {
	etcd.start(true)
}

func (etcd *ETCDClusterRunner) Stop() {
	etcd.stop(true)
}

func (etcd *ETCDClusterRunner) KillWithFire() {
	etcd.kill()
}

func (etcd *ETCDClusterRunner) GoAway() {
	etcd.stop(false)
}

func (etcd *ETCDClusterRunner) ComeBack() {
	etcd.start(false)
}

func (etcd *ETCDClusterRunner) NodeURLS() []string {
	urls := make([]string, etcd.numNodes)
	for i := 0; i < etcd.numNodes; i++ {
		urls[i] = etcd.clientURL(i)
	}
	return urls
}

func (etcd *ETCDClusterRunner) DiskUsage() (bytes int64, err error) {
	fi, err := os.Stat(etcd.tmpPathTo("log", 0))
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func (etcd *ETCDClusterRunner) Reset() {
	etcd.mutex.RLock()
	running := etcd.running
	etcd.mutex.RUnlock()

	if running {
		response, err := etcd.client.Get("/", false, false)
		if err == nil {
			for _, doomed := range response.Node.Nodes {
				etcd.client.Delete(doomed.Key, true)
			}
		}
	}
}

func (etcd *ETCDClusterRunner) ResetAllBut(roots ...string) {
	etcd.mutex.RLock()
	running := etcd.running
	etcd.mutex.RUnlock()

	rootMap := map[string]*struct{}{}
	for _, root := range roots {
		rootMap[root] = &struct{}{}
	}

	if running {
		response, err := etcd.client.Get("/", false, false)
		if err == nil {
			for _, doomed := range response.Node.Nodes {
				if rootMap[doomed.Key] == nil {
					etcd.client.Delete(doomed.Key, true)
				}
			}
		}
	}
}

func (etcd *ETCDClusterRunner) FastForwardTime(seconds int) {
	etcd.mutex.RLock()
	running := etcd.running
	etcd.mutex.RUnlock()

	if running {
		response, err := etcd.client.Get("/", false, true)
		Expect(err).NotTo(HaveOccurred())
		etcd.fastForwardTime(response.Node, seconds)
	}
}

func (etcd *ETCDClusterRunner) newAdapter(clientSSL *SSLConfig) storeadapter.StoreAdapter {
	pool, err := workpool.NewWorkPool(10)
	Expect(err).NotTo(HaveOccurred())

	options := &etcdstoreadapter.ETCDOptions{
		ClusterUrls: etcd.NodeURLS(),
		IsSSL:       false,
	}

	if clientSSL != nil {
		options.CertFile = clientSSL.CertFile
		options.KeyFile = clientSSL.KeyFile
		options.CAFile = clientSSL.CAFile
		options.IsSSL = true
	}

	adapter, err := etcdstoreadapter.New(options, pool)
	Expect(err).NotTo(HaveOccurred())
	return adapter
}

func (etcd *ETCDClusterRunner) Adapter(clientSSL *SSLConfig) storeadapter.StoreAdapter {
	adapter := etcd.newAdapter(clientSSL)
	adapter.Connect()
	return adapter
}

func (etcd *ETCDClusterRunner) RetryableAdapter(workPoolSize int, clientSSL *SSLConfig) storeadapter.StoreAdapter {
	adapter := storeadapter.NewRetryable(
		etcd.newAdapter(clientSSL),
		clock.NewClock(),
		storeadapter.ExponentialRetryPolicy{},
	)

	adapter.Connect()

	return adapter
}

func (etcd *ETCDClusterRunner) start(nuke bool) {
	etcd.mutex.RLock()
	running := etcd.running
	etcd.mutex.RUnlock()

	if running {
		return
	}

	etcd.mutex.Lock()
	defer etcd.mutex.Unlock()

	etcd.etcdProcesses = make([]ifrit.Process, etcd.numNodes)

	clusterURLs := make([]string, etcd.numNodes)
	for i := 0; i < etcd.numNodes; i++ {
		clusterURLs[i] = etcd.nodeName(i) + "=" + etcd.serverURL(i)
	}

	for i := 0; i < etcd.numNodes; i++ {
		if nuke {
			etcd.nukeArtifacts(i)
		}

		if etcd.detectRunningEtcd(i) {
			log.Fatalf("Detected an ETCD already running on %s", etcd.clientURL(i))
		}

		var args []string
		if etcd.serverSSL != nil {
			args = append(args,
				"--cert-file="+etcd.serverSSL.CertFile,
				"--key-file="+etcd.serverSSL.KeyFile,
			)
			if etcd.serverSSL.CAFile != "" {
				args = append(args, "--ca-file="+etcd.serverSSL.CAFile)
			}
		}

		os.MkdirAll(etcd.tmpPath(i), 0700)
		process := ginkgomon.Invoke(ginkgomon.New(ginkgomon.Config{
			Name:              "etcd_cluster",
			AnsiColorCode:     "33m",
			StartCheck:        "etcdserver: published",
			StartCheckTimeout: 10 * time.Second,
			Command: exec.Command(
				"etcd",
				append([]string{
					"--name", etcd.nodeName(i),
					"--data-dir", etcd.tmpPath(i),
					"--listen-client-urls", etcd.clientURL(i),
					"--listen-peer-urls", etcd.serverURL(i),
					"--initial-cluster", strings.Join(clusterURLs, ","),
					"--initial-advertise-peer-urls", etcd.serverURL(i),
					"--initial-cluster-state", "new",
					"--advertise-client-urls", etcd.clientURL(i),
				}, args...)...,
			),
		}))

		etcd.etcdProcesses[i] = process

		Eventually(func() bool {
			defer func() {
				// https://github.com/coreos/go-etcd/issues/114
				recover()
			}()

			return etcd.detectRunningEtcd(i)
		}, 10, 0.05).Should(BeTrue(), "Expected ETCD to be up and running")
	}

	var client *etcdclient.Client
	if etcd.serverSSL == nil {
		client = etcdclient.NewClient(etcd.NodeURLS())
	} else {
		var err error
		client, err = etcdstoreadapter.NewETCDTLSClient(
			etcd.NodeURLS(),
			etcd.serverSSL.CertFile,
			etcd.serverSSL.KeyFile,
			etcd.serverSSL.CAFile,
		)
		Expect(err).NotTo(HaveOccurred())
	}
	etcd.client = client

	etcd.running = true
}

func (etcd *ETCDClusterRunner) stop(nuke bool) {
	etcd.mutex.Lock()
	defer etcd.mutex.Unlock()

	if etcd.running {
		for i := 0; i < etcd.numNodes; i++ {
			ginkgomon.Interrupt(etcd.etcdProcesses[i], 5*time.Second)
			if nuke {
				etcd.nukeArtifacts(i)
			}
		}
		etcd.markAsStopped()
	}
}

func (etcd *ETCDClusterRunner) kill() {
	etcd.mutex.Lock()
	defer etcd.mutex.Unlock()

	if etcd.running {
		for i := 0; i < etcd.numNodes; i++ {
			ginkgomon.Kill(etcd.etcdProcesses[i], 5*time.Second)
			etcd.nukeArtifacts(i)
		}
		etcd.markAsStopped()
	}
}

func (etcd *ETCDClusterRunner) markAsStopped() {
	etcd.etcdProcesses = nil
	etcd.running = false
	etcd.client = nil
}

func (etcd *ETCDClusterRunner) detectRunningEtcd(index int) bool {
	var client *etcdclient.Client

	if etcd.serverSSL == nil {
		client = etcdclient.NewClient([]string{})
	} else {
		var err error
		client, err = etcdstoreadapter.NewETCDTLSClient(
			[]string{etcd.clientURL(index)},
			etcd.serverSSL.CertFile,
			etcd.serverSSL.KeyFile,
			etcd.serverSSL.CAFile,
		)
		Expect(err).NotTo(HaveOccurred())
	}
	return client.SetCluster([]string{etcd.clientURL(index)})
}

func (etcd *ETCDClusterRunner) fastForwardTime(etcdNode *etcdclient.Node, seconds int) {
	if etcdNode.Dir == true {
		for _, child := range etcdNode.Nodes {
			etcd.fastForwardTime(child, seconds)
		}
	} else {
		if etcdNode.TTL == 0 {
			return
		}
		if etcdNode.TTL <= int64(seconds) {
			_, err := etcd.client.Delete(etcdNode.Key, true)
			Expect(err).NotTo(HaveOccurred())
		} else {
			_, err := etcd.client.Set(etcdNode.Key, etcdNode.Value, uint64(etcdNode.TTL-int64(seconds)))
			Expect(err).NotTo(HaveOccurred())
		}
	}
}

func (etcd *ETCDClusterRunner) clientURL(index int) string {
	scheme := "http"
	if etcd.serverSSL != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://127.0.0.1:%d", scheme, etcd.port(index))
}

func (etcd *ETCDClusterRunner) serverURL(index int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", etcd.port(index)+3000)
}

func (etcd *ETCDClusterRunner) nodeName(index int) string {
	return fmt.Sprintf("node%d", index)
}

func (etcd *ETCDClusterRunner) port(index int) int {
	return etcd.startingPort + index
}

func (etcd *ETCDClusterRunner) tmpPath(index int) string {
	return fmt.Sprintf("/tmp/ETCD_%d", etcd.port(index))
}

func (etcd *ETCDClusterRunner) tmpPathTo(subdir string, index int) string {
	return fmt.Sprintf("/%s/%s", etcd.tmpPath(index), subdir)
}

func (etcd *ETCDClusterRunner) nukeArtifacts(index int) {
	os.RemoveAll(etcd.tmpPath(index))
}
