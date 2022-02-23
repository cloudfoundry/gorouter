package monitor

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"code.cloudfoundry.org/gorouter/logger"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/uber-go/zap"
)

type FileDescriptor struct {
	path   string
	ticker *time.Ticker
	sender metrics.MetricSender
	logger logger.Logger
}

func NewFileDescriptor(path string, ticker *time.Ticker, sender metrics.MetricSender, logger logger.Logger) *FileDescriptor {
	return &FileDescriptor{
		path:   path,
		ticker: ticker,
		sender: sender,
		logger: logger,
	}
}

func (f *FileDescriptor) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	close(ready)
	for {
		select {
		case <-f.ticker.C:
			numFds := 0
			if runtime.GOOS == "linux" {
				fdInfo, err := ioutil.ReadDir(f.path)
				if err != nil {
					f.logger.Error("error-reading-filedescriptor-path", zap.Error(err))
					break
				}
				numFds = symlinks(fdInfo)
			} else if runtime.GOOS == "darwin" {
				// no /proc on MacOS, falling back to lsof
				out, err := exec.Command("/bin/sh", "-c", fmt.Sprintf("lsof -p %v", os.Getpid())).Output()
				if err != nil {
					f.logger.Error("error-running-lsof", zap.Error(err))
					break
				}
				lines := strings.Split(string(out), "\n")
				numFds = len(lines) - 1 //cut the table header
			}
			if err := f.sender.SendValue("file_descriptors", float64(numFds), "file"); err != nil {
				f.logger.Error("error-sending-file-descriptor-metric", zap.Error(err))
			}

		case <-signals:
			f.logger.Info("exited")
			return nil
		}
	}
}

func symlinks(fileInfos []os.FileInfo) (count int) {
	for i := 0; i < len(fileInfos); i++ {
		if fileInfos[i].Mode()&os.ModeSymlink == os.ModeSymlink {
			count++
		}
	}
	return count
}
