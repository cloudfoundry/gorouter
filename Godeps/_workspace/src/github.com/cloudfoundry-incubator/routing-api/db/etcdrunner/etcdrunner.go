package etcdrunner

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	etcdclient "github.com/coreos/go-etcd/etcd"
	"github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

type ETCDRunner struct {
	startingPort int
	numNodes     int
	etcdSessions []*gexec.Session
	running      bool
	client       *etcdclient.Client

	mutex *sync.RWMutex
}

func NewETCDRunner(startingPort int, numNodes int) *ETCDRunner {
	return &ETCDRunner{
		startingPort: startingPort,
		numNodes:     numNodes,

		mutex: &sync.RWMutex{},
	}
}

func (etcd *ETCDRunner) Start() {
	etcd.start(true)
}

func (etcd *ETCDRunner) Stop() {
	etcd.stop(true)
}

func (etcd *ETCDRunner) KillWithFire() {
	etcd.kill()
}

func (etcd *ETCDRunner) GoAway() {
	etcd.stop(false)
}

func (etcd *ETCDRunner) ComeBack() {
	etcd.start(false)
}

func (etcd *ETCDRunner) NodeURLS() []string {
	urls := make([]string, etcd.numNodes)
	for i := 0; i < etcd.numNodes; i++ {
		urls[i] = "http://" + etcd.clientUrl(i)
	}
	return urls
}

func (etcd *ETCDRunner) DiskUsage() (bytes int64, err error) {
	fi, err := os.Stat(etcd.tmpPathTo("log", 0))
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func (etcd *ETCDRunner) Reset() {
	etcd.mutex.RLock()
	running := etcd.running
	etcd.mutex.RUnlock()

	if running {
		response, err := etcd.client.Get("/", false, false)
		Ω(err).ShouldNot(HaveOccurred())
		for _, doomed := range response.Node.Nodes {
			etcd.client.Delete(doomed.Key, true)
		}
	}
}

func (etcd *ETCDRunner) FastForwardTime(seconds int) {
	etcd.mutex.RLock()
	running := etcd.running
	etcd.mutex.RUnlock()

	if running {
		response, err := etcd.client.Get("/", false, true)
		Ω(err).ShouldNot(HaveOccurred())
		etcd.fastForwardTime(response.Node, seconds)
	}
}

func (etcd *ETCDRunner) Client() *etcdclient.Client {
	return etcd.client
}

func (etcd *ETCDRunner) start(nuke bool) {
	etcd.mutex.RLock()
	running := etcd.running
	etcd.mutex.RUnlock()

	if running {
		return
	}

	etcd.mutex.Lock()
	defer etcd.mutex.Unlock()

	etcd.etcdSessions = make([]*gexec.Session, etcd.numNodes)

	for i := 0; i < etcd.numNodes; i++ {
		if nuke {
			etcd.nukeArtifacts(i)
		}

		if etcd.detectRunningEtcd(i) {
			log.Fatalf("Detected an ETCD already running on %s", etcd.clientUrl(i))
		}

		os.MkdirAll(etcd.tmpPath(i), 0700)
		args := []string{"-data-dir", etcd.tmpPath(i), "-addr", etcd.clientUrl(i), "-peer-addr", etcd.serverUrl(i), "-name", etcd.nodeName(i)}
		if i != 0 {
			args = append(args, "-peers", etcd.serverUrl(0))
		}

		session, err := gexec.Start(
			exec.Command("etcd", args...),
			gexec.NewPrefixedWriter("\x1b[32m[o]\x1b[33m[etcd_cluster]\x1b[0m ", ginkgo.GinkgoWriter),
			gexec.NewPrefixedWriter("\x1b[91m[e]\x1b[33m[etcd_cluster]\x1b[0m ", ginkgo.GinkgoWriter),
		)
		Ω(err).ShouldNot(HaveOccurred(), "Make sure etcd is compiled and on your $PATH.")

		etcd.etcdSessions[i] = session

		Eventually(func() bool {
			return etcd.detectRunningEtcd(i)
		}, 3, 0.05).Should(BeTrue(), "Expected ETCD to be up and running")
	}

	etcd.client = etcdclient.NewClient(etcd.NodeURLS())
	etcd.running = true
}

func (etcd *ETCDRunner) stop(nuke bool) {
	etcd.mutex.Lock()
	defer etcd.mutex.Unlock()

	if etcd.running {
		for i := 0; i < etcd.numNodes; i++ {
			etcd.etcdSessions[i].Interrupt().Wait(5 * time.Second)
			if nuke {
				etcd.nukeArtifacts(i)
			}
		}
		etcd.markAsStopped()
	}
}

func (etcd *ETCDRunner) kill() {
	etcd.mutex.Lock()
	defer etcd.mutex.Unlock()

	if etcd.running {
		for i := 0; i < etcd.numNodes; i++ {
			etcd.etcdSessions[i].Kill().Wait(5 * time.Second)
			etcd.nukeArtifacts(i)
		}
		etcd.markAsStopped()
	}
}

func (etcd *ETCDRunner) markAsStopped() {
	etcd.etcdSessions = nil
	etcd.running = false
	etcd.client = nil
}

func (etcd *ETCDRunner) detectRunningEtcd(index int) bool {
	client := etcdclient.NewClient([]string{})
	return client.SetCluster([]string{"http://" + etcd.clientUrl(index)})
}

func (etcd *ETCDRunner) fastForwardTime(etcdNode *etcdclient.Node, seconds int) {
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
			Ω(err).ShouldNot(HaveOccurred())
		} else {
			_, err := etcd.client.Set(etcdNode.Key, etcdNode.Value, uint64(etcdNode.TTL-int64(seconds)))
			Ω(err).ShouldNot(HaveOccurred())
		}
	}
}

func (etcd *ETCDRunner) clientUrl(index int) string {
	return fmt.Sprintf("127.0.0.1:%d", etcd.port(index))
}

func (etcd *ETCDRunner) serverUrl(index int) string {
	return fmt.Sprintf("127.0.0.1:%d", etcd.port(index)+3000)
}

func (etcd *ETCDRunner) nodeName(index int) string {
	return fmt.Sprintf("node%d", index)
}

func (etcd *ETCDRunner) port(index int) int {
	return etcd.startingPort + index
}

func (etcd *ETCDRunner) tmpPath(index int) string {
	return fmt.Sprintf("/tmp/ETCD_%d", etcd.port(index))
}

func (etcd *ETCDRunner) tmpPathTo(subdir string, index int) string {
	return fmt.Sprintf("/%s/%s", etcd.tmpPath(index), subdir)
}

func (etcd *ETCDRunner) nukeArtifacts(index int) {
	os.RemoveAll(etcd.tmpPath(index))
}
