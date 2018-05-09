package monitor

import (
	"io/ioutil"
	"os"
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
			fdInfo, err := ioutil.ReadDir(f.path)
			if err != nil {
				f.logger.Error("error-reading-filedescriptor-path", zap.Error(err))
				break
			}

			if err = f.sender.SendValue("file_descriptors", float64(symlinks(fdInfo)), "file"); err != nil {
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
