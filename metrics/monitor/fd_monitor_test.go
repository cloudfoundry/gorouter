package monitor_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics/fakes"
	"code.cloudfoundry.org/gorouter/metrics/monitor"
	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
	"go.uber.org/zap/zapcore"
)

var _ = Describe("FileDescriptor", func() {
	var (
		sender   *fakes.MetricSender
		procPath string
		tr       *time.Ticker
		testSink *test_util.TestSink
		logger   *slog.Logger
	)

	BeforeEach(func() {
		tr = time.NewTicker(1 * time.Second)
		sender = &fakes.MetricSender{}
		logger = log.CreateLogger()
		testSink = &test_util.TestSink{Buffer: gbytes.NewBuffer()}
		log.SetDynamicWriteSyncer(zapcore.NewMultiWriteSyncer(testSink, zapcore.AddSync(GinkgoWriter)))
		log.SetLoggingLevel("Debug")
	})

	AfterEach(func() {
		tr.Stop()
		Expect(os.RemoveAll(procPath)).To(Succeed())
	})

	It("exits when os signal is received", func() {
		fdMonitor := monitor.NewFileDescriptor(procPath, tr, sender, logger)
		process := ifrit.Invoke(fdMonitor)
		Eventually(process.Ready()).Should(BeClosed())

		process.Signal(os.Interrupt)
		var err error
		Eventually(process.Wait()).Should(Receive(&err))
		Expect(err).ToNot(HaveOccurred())

	})

	It("monitors all the open file descriptors for a given pid", func() {
		procPath = createTestPath("", 10)
		fdMonitor := monitor.NewFileDescriptor(procPath, tr, sender, logger)
		process := ifrit.Invoke(fdMonitor)
		Eventually(process.Ready()).Should(BeClosed())

		Eventually(sender.SendValueCallCount, "2s").Should(Equal(1))
		name, value, unit := sender.SendValueArgsForCall(0)
		Expect(name).To(Equal("file_descriptors"))
		Expect(value).To(BeEquivalentTo(10))
		Expect(unit).To(Equal("file"))

		// create some more FDs
		createTestPath(procPath, 20)

		Eventually(sender.SendValueCallCount, "2s").Should(Equal(2))
		name, value, unit = sender.SendValueArgsForCall(1)
		Expect(name).To(Equal("file_descriptors"))
		Expect(value).To(BeEquivalentTo(20))
		Expect(unit).To(Equal("file"))
	})
})

func createTestPath(path string, symlink int) string {
	// Create symlink structure similar to /proc/pid/fd in linux file system
	createSymlink := func(dir string, n int) {
		fd, err := os.CreateTemp(dir, "socket")
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
	procPath, err := os.MkdirTemp("", "proc")
	Expect(err).NotTo(HaveOccurred())
	createSymlink(procPath, symlink)
	return procPath
}
