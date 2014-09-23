package fake

import (
	"bufio"
	"io"
	"sync"
)

type FakeLogSender struct {
	logs        []Log
	ReturnError error
	sync.RWMutex
}

type Log struct {
	AppId          string
	Message        string
	SourceType     string
	SourceInstance string
	MessageType    string
}

func NewFakeLogSender() *FakeLogSender {
	return &FakeLogSender{}
}

func (fls *FakeLogSender) SendAppLog(appId, message, sourceType, sourceInstance string) error {
	fls.Lock()
	defer fls.Unlock()

	if fls.ReturnError != nil {
		err := fls.ReturnError
		fls.ReturnError = nil

		return err
	}

	fls.logs = append(fls.logs, Log{AppId: appId, Message: message, SourceType: sourceType, SourceInstance: sourceInstance, MessageType: "OUT"})
	return nil
}

func (fls *FakeLogSender) SendAppErrorLog(appId, message, sourceType, sourceInstance string) error {
	fls.Lock()
	defer fls.Unlock()

	if fls.ReturnError != nil {
		err := fls.ReturnError
		fls.ReturnError = nil

		return err
	}

	fls.logs = append(fls.logs, Log{AppId: appId, Message: message, SourceType: sourceType, SourceInstance: sourceInstance, MessageType: "ERR"})
	return nil
}

func (fls *FakeLogSender) ScanLogStream(appId, sourceType, sourceInstance string, reader io.Reader, stopChan chan struct{}) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fls.Lock()
		msg := scanner.Text()
		if len(msg) == 0 {
			fls.Unlock()
			continue
		}

		fls.logs = append(fls.logs, Log{AppId: appId, SourceType: sourceType, SourceInstance: sourceInstance, MessageType: "OUT", Message: msg})
		fls.Unlock()
	}
}

func (fls *FakeLogSender) ScanErrorLogStream(appId, sourceType, sourceInstance string, reader io.Reader, stopChan chan struct{}) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {

		fls.Lock()

		msg := scanner.Text()
		if len(msg) == 0 {
			fls.Unlock()
			continue
		}

		fls.logs = append(fls.logs, Log{AppId: appId, SourceType: sourceType, SourceInstance: sourceInstance, MessageType: "ERR", Message: msg})
		fls.Unlock()
	}
}

func (fls *FakeLogSender) GetLogs() []Log {
	fls.Lock()
	defer fls.Unlock()

	return fls.logs
}
