package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry-incubator/routing-api/models"
	uaaclient "github.com/cloudfoundry-incubator/uaa-go-client"
	"github.com/pivotal-golang/lager"
)

type TcpRouteMappingsHandler struct {
	uaaClient uaaclient.Client
	validator RouteValidator
	db        db.DB
	logger    lager.Logger
	maxTTL    int
}

func NewTcpRouteMappingsHandler(uaaClient uaaclient.Client, validator RouteValidator, database db.DB, ttl int, logger lager.Logger) *TcpRouteMappingsHandler {
	return &TcpRouteMappingsHandler{
		uaaClient: uaaClient,
		validator: validator,
		db:        database,
		logger:    logger,
		maxTTL:    ttl,
	}
}

func (h *TcpRouteMappingsHandler) List(w http.ResponseWriter, req *http.Request) {
	log := h.logger.Session("list-tcp-route-mappings")

	err := h.uaaClient.DecodeToken(req.Header.Get("Authorization"), RoutingRoutesReadScope)
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

	var tcpMappings []models.TcpRouteMapping
	err := decoder.Decode(&tcpMappings)
	if err != nil {
		handleProcessRequestError(w, err, log)
		return
	}

	log.Info("request", lager.Data{"tcp_mapping_creation": tcpMappings})

	err = h.uaaClient.DecodeToken(req.Header.Get("Authorization"), RoutingRoutesWriteScope)
	if err != nil {
		handleUnauthorizedError(w, err, log)
		return
	}

	// fetch current router groups
	routerGroups, err := h.db.ReadRouterGroups()
	if err != nil {
		handleDBCommunicationError(w, err, log)
		return
	}

	apiErr := h.validator.ValidateCreateTcpRouteMapping(tcpMappings, routerGroups, uint16(h.maxTTL))
	if apiErr != nil {
		handleProcessRequestError(w, apiErr, log)
		return
	}

	for _, tcpMapping := range tcpMappings {
		err = h.db.SaveTcpRouteMapping(tcpMapping)
		if err != nil {
			if err == db.ErrorConflict {
				handleDBConflictError(w, err, log)
			} else {
				handleDBCommunicationError(w, err, log)
			}
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *TcpRouteMappingsHandler) Delete(w http.ResponseWriter, req *http.Request) {
	log := h.logger.Session("delete-tcp-route-mappings")
	decoder := json.NewDecoder(req.Body)

	var tcpMappings []models.TcpRouteMapping
	err := decoder.Decode(&tcpMappings)
	if err != nil {
		handleProcessRequestError(w, err, log)
		return
	}

	log.Info("request", lager.Data{"tcp_mapping_deletion": tcpMappings})

	err = h.uaaClient.DecodeToken(req.Header.Get("Authorization"), RoutingRoutesWriteScope)
	if err != nil {
		handleUnauthorizedError(w, err, log)
		return
	}

	apiErr := h.validator.ValidateDeleteTcpRouteMapping(tcpMappings)
	if apiErr != nil {
		handleProcessRequestError(w, apiErr, log)
		return
	}

	for _, tcpMapping := range tcpMappings {
		err = h.db.DeleteTcpRouteMapping(tcpMapping)
		if err != nil {
			if dberr, ok := err.(db.DBError); !ok || dberr.Type != db.KeyNotFound {
				handleDBCommunicationError(w, err, log)
				return
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
