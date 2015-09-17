package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/routing-api/authentication"
	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/pivotal-golang/lager"
)

type TcpRouteMappingsHandler struct {
	token     authentication.Token
	validator RouteValidator
	db        db.DB
	logger    lager.Logger
}

func NewTcpRouteMappingsHandler(token authentication.Token, validator RouteValidator, database db.DB, logger lager.Logger) *TcpRouteMappingsHandler {
	return &TcpRouteMappingsHandler{
		token:     token,
		validator: validator,
		db:        database,
		logger:    logger,
	}
}

func (h *TcpRouteMappingsHandler) List(w http.ResponseWriter, req *http.Request) {
	log := h.logger.Session("list-tcp-route-mappings")

	err := h.token.DecodeToken(req.Header.Get("Authorization"), RoutingRoutesReadScope)
	if err != nil {
		handleUnauthorizedError(w, err, log)
		return
	}
	routes, err := h.db.ReadTcpRouteMappings()
	if err != nil {
		handleDBCommunicationError(w, err, log)
		return
	}
	encoder := json.NewEncoder(w)
	encoder.Encode(routes)
}

func (h *TcpRouteMappingsHandler) Upsert(w http.ResponseWriter, req *http.Request) {
	log := h.logger.Session("create-tcp-route-mappings")
	decoder := json.NewDecoder(req.Body)

	var tcpMappings []db.TcpRouteMapping
	err := decoder.Decode(&tcpMappings)
	if err != nil {
		handleProcessRequestError(w, err, log)
		return
	}

	log.Info("request", lager.Data{"tcp_mapping_creation": tcpMappings})

	err = h.token.DecodeToken(req.Header.Get("Authorization"), RoutingRoutesWriteScope)
	if err != nil {
		handleUnauthorizedError(w, err, log)
		return
	}

	apiErr := h.validator.ValidateTcpRouteMapping(tcpMappings)
	if apiErr != nil {
		handleProcessRequestError(w, apiErr, log)
		return
	}

	for _, tcpMapping := range tcpMappings {
		err = h.db.SaveTcpRouteMapping(tcpMapping)
		if err != nil {
			handleDBCommunicationError(w, err, log)
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
}
