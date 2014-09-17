package log_sender

import (
	"code.google.com/p/gogoprotobuf/proto"
	"github.com/cloudfoundry/dropsonde/emitter"
	"github.com/cloudfoundry/dropsonde/events"
	"time"
)

// A LogSender emits log events.
type LogSender interface {
	SendAppLog(appId, message, sourceType, sourceInstance string) error
	SendAppErrorLog(appId, message, sourceType, sourceInstance string) error
}

type logSender struct {
	eventEmitter emitter.EventEmitter
}

// NewLogSender instantiates a logSender with the given EventEmitter.
func NewLogSender(eventEmitter emitter.EventEmitter) LogSender {
	return &logSender{eventEmitter: eventEmitter}
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
