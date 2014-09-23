package log_sender

import (
	"bufio"
	"code.google.com/p/gogoprotobuf/proto"
	"fmt"
	"github.com/cloudfoundry/dropsonde/emitter"
	"github.com/cloudfoundry/dropsonde/events"
	"github.com/cloudfoundry/gosteno"
	"io"
	"time"
)

// A LogSender emits log events.
type LogSender interface {
	SendAppLog(appId, message, sourceType, sourceInstance string) error
	SendAppErrorLog(appId, message, sourceType, sourceInstance string) error

	ScanLogStream(appId, sourceType, sourceInstance string, reader io.Reader, stopChan chan struct{})
	ScanErrorLogStream(appId, sourceType, sourceInstance string, reader io.Reader, stopChan chan struct{})
}

type logSender struct {
	eventEmitter emitter.EventEmitter
	logger       *gosteno.Logger
}

// NewLogSender instantiates a logSender with the given EventEmitter.
func NewLogSender(eventEmitter emitter.EventEmitter, logger *gosteno.Logger) LogSender {
	return &logSender{eventEmitter: eventEmitter, logger: logger}
}

// SendAppLog sends a log message with the given appid and log message
// with a message type of std out.
// Returns an error if one occurs while sending the event.
func (l *logSender) SendAppLog(appId, message, sourceType, sourceInstance string) error {
	return l.eventEmitter.Emit(makeLogMessage(appId, message, sourceType, sourceInstance, events.LogMessage_OUT))
}

// SendAppErrorLog sends a log error message with the given appid and log message
// with a message type of std err.
// Returns an error if one occurs while sending the event.
func (l *logSender) SendAppErrorLog(appId, message, sourceType, sourceInstance string) error {
	return l.eventEmitter.Emit(makeLogMessage(appId, message, sourceType, sourceInstance, events.LogMessage_ERR))
}

// ScanLogStream sends a log message with the given meta-data for each line from reader.
// Restarts on read errors and continues until EOF (or stopChan is closed).
func (l *logSender) ScanLogStream(appId, sourceType, sourceInstance string, reader io.Reader, stopChan chan struct{}) {
	l.scanLogStream(appId, sourceType, sourceInstance, l.SendAppLog, reader, stopChan)
}

// ScanErrorLogStream sends a log error message with the given meta-data for each line from reader.
// Restarts on read errors and continues until EOF (or stopChan is closed).
func (l *logSender) ScanErrorLogStream(appId, sourceType, sourceInstance string, reader io.Reader, stopChan chan struct{}) {
	l.scanLogStream(appId, sourceType, sourceInstance, l.SendAppErrorLog, reader, stopChan)
}

func (l *logSender) scanLogStream(appId, sourceType, sourceInstance string, send func(string, string, string, string) error, reader io.Reader, stopChan chan struct{}) {
	for {
		scanner := bufio.NewScanner(reader)

		for scanner.Scan() {
			select {
			case <-stopChan:
				return
			default:
			}

			line := scanner.Text()

			if len(line) == 0 {
				continue
			}

			send(appId, line, sourceType, sourceInstance)
		}

		err := scanner.Err()
		if err != nil {
			l.logger.Errorf("ScanLogStream: Error while reading STDOUT/STDERR for app %s/%s: %s", appId, sourceInstance, err.Error())
			msg := fmt.Sprintf("Dropped log message due to read error: %s", err.Error())
			l.SendAppErrorLog(appId, msg, sourceType, sourceInstance)
		} else {
			l.logger.Debugf("EOF on log stream for app %s/%s", appId, sourceInstance)
			return
		}
	}
}

func makeLogMessage(appId, message, sourceType, sourceInstance string, messageType events.LogMessage_MessageType) *events.LogMessage {
	return &events.LogMessage{
		Message:        []byte(message),
		AppId:          proto.String(appId),
		MessageType:    &messageType,
		SourceType:     &sourceType,
		SourceInstance: &sourceInstance,
		Timestamp:      proto.Int64(time.Now().UnixNano()),
	}
}
