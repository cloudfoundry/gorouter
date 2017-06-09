package monitor_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/metrics/monitor"
	"code.cloudfoundry.org/gorouter/test_util"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
)

var _ = Describe("FileDescriptor", func() {
	var (
		sender            *fakes.MetricSender
		ch                chan time.Time
		testTickerHarness monitor.Ticker
		procPath          string
		logger            logger.Logger
	)

	BeforeEach(func() {
		sender = new(fakes.MetricSender)
		ch = make(chan time.Time)
		testTickerHarness = func(d time.Duration) <-chan time.Time {
			return ch
		}
		logger = test_util.NewTestZapLogger("test")
	})

	AfterEach(func() {
		Expect(os.RemoveAll(procPath)).To(Succeed())
	})

	It("exists when os signal is received", func() {
		fdMonintor := monitor.NewFileDescriptor(procPath, testTickerHarness(1), sender, logger)
		process := ifrit.Invoke(fdMonintor)
		Eventually(process.Ready()).Should(BeClosed())

		process.Signal(os.Interrupt)
		var err error
		Eventually(process.Wait()).Should(Receive(&err))
		Expect(err).ToNot(HaveOccurred())

	})

	It("monitors all the open file descriptors for a given pid", func() {
		procPath = createTestPath("", 10)
		fdMonintor := monitor.NewFileDescriptor(procPath, testTickerHarness(1), sender, logger)
		process := ifrit.Invoke(fdMonintor)
		Eventually(process.Ready()).Should(BeClosed())

		ch <- time.Time{}
		ch <- time.Time{}

		Expect(sender.SendValueCallCount()).To(Equal(1))
		name, value, unit := sender.SendValueArgsForCall(0)
		Expect(name).To(Equal("file_descriptors"))
		Expect(value).To(BeEquivalentTo(10))
		Expect(unit).To(Equal("file"))

		// create some more FDs
		createTestPath(procPath, 20)

		ch <- time.Time{}
		ch <- time.Time{}
		Expect(sender.SendValueCallCount()).To(Equal(3))
		name, value, unit = sender.SendValueArgsForCall(2)
		Expect(name).To(Equal("file_descriptors"))
		Expect(value).To(BeEquivalentTo(20))
		Expect(unit).To(Equal("file"))
	})

})

func createTestPath(path string, symlink int) string {
	// Create symlink structure similar to /proc/pid/fd in linux file system
	createSymlink := func(dir string, n int) {
		fd, err := ioutil.TempFile(dir, "socket")
		Expect(err).NotTo(HaveOccurred())
		for i := 0; i < n; i++ {
			fdId := strconv.Itoa(i)
			symlink := filepath.Join(dir, fdId)
			os.Symlink(fd.Name()+fdId, symlink)

		}
	}
	if path != "" {
		createSymlink(path, symlink)
		return path
	}
	procPath, err := ioutil.TempDir("", "proc")
	Expect(err).NotTo(HaveOccurred())
	createSymlink(procPath, symlink)
	return procPath
}
