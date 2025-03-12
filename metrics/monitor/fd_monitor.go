package monitor

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	log "code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/metrics"
)

type FileDescriptor struct {
	path     string
	ticker   *time.Ticker
	reporter metrics.MonitorReporter
	logger   *slog.Logger
}

func NewFileDescriptor(path string, ticker *time.Ticker, reporter metrics.MonitorReporter, logger *slog.Logger) *FileDescriptor {
	return &FileDescriptor{
		path:     path,
		ticker:   ticker,
		reporter: reporter,
		logger:   logger,
	}
}

func (f *FileDescriptor) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	close(ready)
	for {
		select {
		case <-f.ticker.C:
			numFds := 0
			if runtime.GOOS == "linux" {
				dirEntries, err := os.ReadDir(f.path)
				if err != nil {
					f.logger.Error("error-reading-filedescriptor-path", log.ErrAttr(err))
					break
				}
				numFds = symlinks(dirEntries)
			} else if runtime.GOOS == "darwin" {
				dirEntries, err := os.ReadDir(f.path)
				if err != nil {
					// no /proc on MacOS, falling back to lsof
					out, err := exec.Command("/bin/sh", "-c", fmt.Sprintf("lsof -p %v", os.Getpid())).Output()
					if err != nil {
						f.logger.Error("error-running-lsof", log.ErrAttr(err))
						break
					}
					lines := strings.Split(string(out), "\n")
					numFds = len(lines) - 1 //cut the table header
				} else {
					numFds = symlinks(dirEntries)
				}
			}
			f.reporter.CaptureFoundFileDescriptors(numFds)

		case <-signals:
			f.logger.Info("exited")
			return nil
		}
	}
}

func symlinks(fileInfos []os.DirEntry) (count int) {
	for i := 0; i < len(fileInfos); i++ {
		if fileInfos[i].Type()&os.ModeSymlink == os.ModeSymlink {
			count++
		}
	}
	return count
}
