package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/cloudfoundry-incubator/routing-api/authentication"
	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry-incubator/routing-api/metrics"
	"github.com/cloudfoundry/storeadapter"
	"github.com/pivotal-golang/lager"
	"github.com/vito/go-sse/sse"
)

type EventStreamHandler struct {
	token    authentication.Token
	db       db.DB
	logger   lager.Logger
	stats    metrics.PartialStatsdClient
	stopChan <-chan struct{}
}

func NewEventStreamHandler(token authentication.Token, database db.DB, logger lager.Logger, stats metrics.PartialStatsdClient, stopChan <-chan struct{}) *EventStreamHandler {
	return &EventStreamHandler{
		token:    token,
		db:       database,
		logger:   logger,
		stats:    stats,
		stopChan: stopChan,
	}
}

func (h *EventStreamHandler) EventStream(w http.ResponseWriter, req *http.Request) {
	h.stats.GaugeDelta("total_subscriptions", 1, 1.0)
	defer h.stats.GaugeDelta("total_subscriptions", -1, 1.0)
	log := h.logger.Session("event-stream-handler")
	h.handleEventStream(log, db.HTTP_ROUTE_BASE_KEY, w, req)
}

func (h *EventStreamHandler) TcpEventStream(w http.ResponseWriter, req *http.Request) {
	h.stats.GaugeDelta("total_tcp_subscriptions", 1, 1.0)
	defer h.stats.GaugeDelta("total_tcp_subscriptions", -1, 1.0)
	log := h.logger.Session("tcp-event-stream-handler")
	h.handleEventStream(log, db.TCP_MAPPING_BASE_KEY, w, req)
}

func (h *EventStreamHandler) handleEventStream(log lager.Logger, filterKey string,
	w http.ResponseWriter, req *http.Request) {

	err := h.token.DecodeToken(req.Header.Get("Authorization"), RoutingRoutesReadScope)
	if err != nil {
		handleUnauthorizedError(w, err, log)
		return
	}
	flusher := w.(http.Flusher)
	closeNotifier := w.(http.CloseNotifier).CloseNotify()

	resultChan, cancelChan, errChan := h.db.WatchRouteChanges(filterKey)

	w.Header().Add("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Add("Connection", "keep-alive")

	w.WriteHeader(http.StatusOK)

	flusher.Flush()

	eventID := 0
	for {
		select {
		case event := <-resultChan:
			eventType, err := stringifyEventType(event.Type)
			if eventType == "Invalid" || err != nil {
				return
			}

			var nodeValue []byte
			switch eventType {
			case "Delete":
				nodeValue = event.PrevNode.Value
			case "Create":
				nodeValue = event.Node.Value
				eventType = "Upsert"
			case "Update":
				nodeValue = event.Node.Value
				eventType = "Upsert"
			}

			err = sse.Event{
				ID:   strconv.Itoa(eventID),
				Name: string(eventType),
				Data: nodeValue,
			}.Write(w)

			if err != nil {
				break
			}

			flusher.Flush()

			eventID++
		case err := <-errChan:
			log.Error("watch-error", err)
			return
		case <-h.stopChan:
			log.Info("event-stream-stopped")
			cancelChan <- true
			return
		case <-closeNotifier:
			log.Info("connection-closed")
			return
		}
	}
}

func stringifyEventType(eventType storeadapter.EventType) (string, error) {
	switch eventType {
	case storeadapter.InvalidEvent:
		return "Invalid", nil
	case storeadapter.CreateEvent:
		return "Create", nil
	case storeadapter.UpdateEvent:
		return "Update", nil
	case storeadapter.DeleteEvent, storeadapter.ExpireEvent:
		return "Delete", nil
	default:
		return "", errors.New("Unknown event type")
	}
}
