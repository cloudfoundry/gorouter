package accesslog

import (
	"strconv"
	"time"

	"code.cloudfoundry.org/gorouter/accesslog/schema"
	"code.cloudfoundry.org/gorouter/config"
	goRouterLogger "code.cloudfoundry.org/gorouter/logger"
	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/emitter"
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/gogo/protobuf/proto"
	"github.com/uber-go/zap"
)

type DropsondeLogSender struct {
	eventEmitter   dropsonde.EventEmitter
	sourceInstance string
	logger         goRouterLogger.Logger
}

func (l *DropsondeLogSender) SendAppLog(appID, message string, tags map[string]string) {
	if l.sourceInstance == "" || appID == "" {
		return
	}

	sourceType := "RTR"
	messageType := events.LogMessage_OUT
	logMessage := &events.LogMessage{
		Message:        []byte(message),
		AppId:          proto.String(appID),
		MessageType:    &messageType,
		SourceType:     &sourceType,
		SourceInstance: &l.sourceInstance,
		Timestamp:      proto.Int64(time.Now().UnixNano()),
	}

	envelope, err := emitter.Wrap(logMessage, l.eventEmitter.Origin())
	if err != nil {
		l.logger.Error("error-wrapping-access-log-for-emitting", zap.Error(err))
		return
	}

	if err = l.eventEmitter.EmitEnvelope(envelope); err != nil {
		l.logger.Error("error-emitting-access-log-to-writers", zap.Error(err))
	}
}

func NewLogSender(
	c *config.Config,
	e dropsonde.EventEmitter,
	logger goRouterLogger.Logger,
) schema.LogSender {
	var dropsondeSourceInstance string

	if c.Logging.LoggregatorEnabled {
		dropsondeSourceInstance = strconv.FormatUint(uint64(c.Index), 10)
	}

	return &DropsondeLogSender{
		e, dropsondeSourceInstance, logger,
	}
}
