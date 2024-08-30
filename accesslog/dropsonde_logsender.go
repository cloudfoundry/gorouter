package accesslog

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"code.cloudfoundry.org/gorouter/accesslog/schema"
	"code.cloudfoundry.org/gorouter/config"
	log "code.cloudfoundry.org/gorouter/logger"
	"github.com/cloudfoundry/dropsonde"
	"github.com/cloudfoundry/dropsonde/emitter"
	"github.com/cloudfoundry/sonde-go/events"
	"google.golang.org/protobuf/proto"
)

type DropsondeLogSender struct {
	eventEmitter   dropsonde.EventEmitter
	sourceInstance string
	logger         *slog.Logger
}

func (l *DropsondeLogSender) SendAppLog(appID, message string, tags map[string]string) {
	if l.sourceInstance == "" || appID == "" {
		l.logger.Debug("dropping-loggregator-access-log",
			log.ErrAttr(fmt.Errorf("either no appId or source instance present")),
			slog.String("appID", appID),
			slog.String("sourceInstance", l.sourceInstance),
		)

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
		l.logger.Error("error-wrapping-access-log-for-emitting", log.ErrAttr(err))
		return
	}

	envelope.Tags = tags

	if err = l.eventEmitter.EmitEnvelope(envelope); err != nil {
		l.logger.Error("error-emitting-access-log-to-writers", log.ErrAttr(err))
	}
}

func NewLogSender(
	c *config.Config,
	e dropsonde.EventEmitter,
	logger *slog.Logger,
) schema.LogSender {
	var dropsondeSourceInstance string

	if c.Logging.LoggregatorEnabled {
		dropsondeSourceInstance = strconv.FormatUint(uint64(c.Index), 10)
	}

	return &DropsondeLogSender{
		e, dropsondeSourceInstance, logger,
	}
}
