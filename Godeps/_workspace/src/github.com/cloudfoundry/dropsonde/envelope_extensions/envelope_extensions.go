package envelope_extensions

import (
	"encoding/binary"
	"fmt"
	"github.com/cloudfoundry/dropsonde/events"
)

const SystemAppId = "system"

type hasAppId interface {
	GetApplicationId() *events.UUID
}

func GetAppId(m *events.Envelope) string {
	if m.GetEventType() == events.Envelope_LogMessage {
		logMessage := m.GetLogMessage()
		return logMessage.GetAppId()
	}

	var event hasAppId
	switch m.GetEventType() {
	case events.Envelope_HttpStart:
		event = m.GetHttpStart()
	case events.Envelope_HttpStop:
		event = m.GetHttpStop()
	case events.Envelope_HttpStartStop:
		event = m.GetHttpStartStop()
	default:
		return SystemAppId
	}

	uuid := event.GetApplicationId()
	if uuid != nil {
		return formatUUID(uuid)
	}
	return SystemAppId
}

func formatUUID(id *events.UUID) string {
	var u [16]byte
	binary.LittleEndian.PutUint64(u[:8], id.GetLow())
	binary.LittleEndian.PutUint64(u[8:], id.GetHigh())
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
}
