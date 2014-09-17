package events

import (
	"encoding/binary"
	"fmt"
)

const SystemAppId = "system"

type hasAppId interface {
	GetApplicationId() *UUID
}

func (m *Envelope) GetAppId() string {
	if m.GetEventType() == Envelope_LogMessage {
		logMessage := m.GetLogMessage()
		return logMessage.GetAppId()
	}

	var event hasAppId
	switch m.GetEventType() {
	case Envelope_HttpStart:
		event = m.GetHttpStart()
	case Envelope_HttpStop:
		event = m.GetHttpStop()
	case Envelope_HttpStartStop:
		event = m.GetHttpStartStop()
	default:
		return SystemAppId
	}

	uuid := event.GetApplicationId()
	if uuid != nil {
		return uuid.FormattedString()
	}
	return SystemAppId
}

func (id *UUID) FormattedString() string {
	var u [16]byte
	binary.LittleEndian.PutUint64(u[:8], id.GetLow())
	binary.LittleEndian.PutUint64(u[8:], id.GetHigh())
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
}
