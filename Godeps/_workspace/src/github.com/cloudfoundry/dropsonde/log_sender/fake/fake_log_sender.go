package fake

import (
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
	panic("Not implemented!")
}

func (fls *FakeLogSender) ScanErrorLogStream(appId, sourceType, sourceInstance string, reader io.Reader, stopChan chan struct{}) {
	panic("Not implemented!")
}

func (fls *FakeLogSender) GetLogs() []Log {
	fls.Lock()
	defer fls.Unlock()

	return fls.logs
}
