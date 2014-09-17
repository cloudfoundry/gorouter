package factories

import (
	"code.google.com/p/gogoprotobuf/proto"
	"encoding/binary"
	"fmt"
	"github.com/cloudfoundry/dropsonde/events"
	uuid "github.com/nu7hatch/gouuid"
	"net/http"
	"strconv"
	"time"
)

func NewUUID(id *uuid.UUID) *events.UUID {
	return &events.UUID{Low: proto.Uint64(binary.LittleEndian.Uint64(id[:8])), High: proto.Uint64(binary.LittleEndian.Uint64(id[8:]))}
}

func NewHttpStart(req *http.Request, peerType events.PeerType, requestId *uuid.UUID) *events.HttpStart {
	httpStart := &events.HttpStart{
		Timestamp:     proto.Int64(time.Now().UnixNano()),
		RequestId:     NewUUID(requestId),
		PeerType:      &peerType,
		Method:        events.Method(events.Method_value[req.Method]).Enum(),
		Uri:           proto.String(fmt.Sprintf("%s%s", req.Host, req.URL.Path)),
		RemoteAddress: proto.String(req.RemoteAddr),
		UserAgent:     proto.String(req.UserAgent()),
	}

	if applicationId, err := uuid.ParseHex(req.Header.Get("X-CF-ApplicationID")); err == nil {
		httpStart.ApplicationId = NewUUID(applicationId)
	}

	if instanceIndex, err := strconv.Atoi(req.Header.Get("X-CF-InstanceIndex")); err == nil {
		httpStart.InstanceIndex = proto.Int(instanceIndex)
	}

	if instanceId := req.Header.Get("X-CF-InstanceID"); instanceId != "" {
		httpStart.InstanceId = &instanceId
	}

	return httpStart
}

func NewHttpStop(req *http.Request, statusCode int, contentLength int64, peerType events.PeerType, requestId *uuid.UUID) *events.HttpStop {
	httpStop := &events.HttpStop{
		Timestamp:     proto.Int64(time.Now().UnixNano()),
		Uri:           proto.String(fmt.Sprintf("%s%s", req.Host, req.URL.Path)),
		RequestId:     NewUUID(requestId),
		PeerType:      &peerType,
		StatusCode:    proto.Int(statusCode),
		ContentLength: proto.Int64(contentLength),
	}

	if applicationId, err := uuid.ParseHex(req.Header.Get("X-CF-ApplicationID")); err == nil {
		httpStop.ApplicationId = NewUUID(applicationId)
	}

	return httpStop
}

func NewHeartbeat(sentCount, receivedCount, errorCount uint64) *events.Heartbeat {
	return &events.Heartbeat{
		SentCount:     proto.Uint64(sentCount),
		ReceivedCount: proto.Uint64(receivedCount),
		ErrorCount:    proto.Uint64(errorCount),
	}
}

func NewLogMessage(messageType events.LogMessage_MessageType, messageString, appId, sourceType string) *events.LogMessage {
	currentTime := time.Now()

	logMessage := &events.LogMessage{
		Message:     []byte(messageString),
		AppId:       &appId,
		MessageType: &messageType,
		SourceType:  proto.String(sourceType),
		Timestamp:   proto.Int64(currentTime.UnixNano()),
	}

	return logMessage
}
