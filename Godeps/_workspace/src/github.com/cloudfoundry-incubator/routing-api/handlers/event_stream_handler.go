package handlers

import (
	"net/http"
	"strconv"

	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry-incubator/routing-api/metrics"
	uaaclient "github.com/cloudfoundry-incubator/uaa-go-client"
	"github.com/pivotal-golang/lager"
	"github.com/vito/go-sse/sse"
)

type EventStreamHandler struct {
	uaaClient uaaclient.Client
	db        db.DB
	logger    lager.Logger
	stats     metrics.PartialStatsdClient
	stopChan  <-chan struct{}
}

func NewEventStreamHandler(uaaClient uaaclient.Client, database db.DB, logger lager.Logger, stats metrics.PartialStatsdClient) *EventStreamHandler {
	return &EventStreamHandler{
		uaaClient: uaaClient,
		db:        database,
		logger:    logger,
		stats:     stats,
	}
}

func (h *EventStreamHandler) EventStream(w http.ResponseWriter, req *http.Request) {
	h.stats.GaugeDelta(metrics.TotalHttpSubscriptions, 1, 1.0)
	defer h.stats.GaugeDelta(metrics.TotalHttpSubscriptions, -1, 1.0)
	log := h.logger.Session("event-stream-handler")
	h.handleEventStream(log, db.HTTP_ROUTE_BASE_KEY, w, req)
}

func (h *EventStreamHandler) TcpEventStream(w http.ResponseWriter, req *http.Request) {
	h.stats.GaugeDelta(metrics.TotalTcpSubscriptions, 1, 1.0)
	defer h.stats.GaugeDelta(metrics.TotalTcpSubscriptions, -1, 1.0)
	log := h.logger.Session("tcp-event-stream-handler")
	h.handleEventStream(log, db.TCP_MAPPING_BASE_KEY, w, req)
}

func (h *EventStreamHandler) handleEventStream(log lager.Logger, filterKey string,
	w http.ResponseWriter, req *http.Request) {

	err := h.uaaClient.DecodeToken(req.Header.Get("Authorization"), RoutingRoutesReadScope)
	if err != nil {
		handleUnauthorizedError(w, err, log)
		return
	}
	flusher := w.(http.Flusher)
	closeNotifier := w.(http.CloseNotifier).CloseNotify()

	resultChan, errChan, cancelFunc := h.db.WatchRouteChanges(filterKey)

	w.Header().Add("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Add("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Add("Connection", "keep-alive")

	w.WriteHeader(http.StatusOK)

	flusher.Flush()

	eventID := 0
	for {
		select {
		case event := <-resultChan:
			eventType := event.Type
			if eventType == db.InvalidEvent {
				h.logger.Info("invalid-event", lager.Data{"event": event})
				return
			}

			var nodeValue string
			switch eventType {
			case db.DeleteEvent, db.ExpireEvent:
				nodeValue = event.PrevNode.Value
			case db.CreateEvent:
				nodeValue = event.Node.Value
			case db.UpdateEvent:
				nodeValue = event.Node.Value
			}

			err = sse.Event{
				ID:   strconv.Itoa(eventID),
				Name: eventType.String(),
				Data: []byte(nodeValue),
			}.Write(w)

			if err != nil {
				break
			}

			flusher.Flush()

			eventID++
		case err := <-errChan:
			log.Error("watch-error", err)
			return
		case <-closeNotifier:
			log.Info("connection-closed")
			cancelFunc()
			return
		}
	}
}
