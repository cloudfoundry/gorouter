package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/cloudfoundry-incubator/routing-api/authentication"
	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/pivotal-golang/lager"
)

type RoutesHandler struct {
	token     authentication.Token
	maxTTL    int
	validator RouteValidator
	db        db.DB
	logger    lager.Logger
}

func NewRoutesHandler(token authentication.Token, maxTTL int, validator RouteValidator, database db.DB, logger lager.Logger) *RoutesHandler {
	return &RoutesHandler{
		token:     token,
		maxTTL:    maxTTL,
		validator: validator,
		db:        database,
		logger:    logger,
	}
}

func (h *RoutesHandler) List(w http.ResponseWriter, req *http.Request) {
	log := h.logger.Session("list-routes")

	err := h.token.DecodeToken(req.Header.Get("Authorization"), RoutingRoutesReadScope)
	if err != nil {
		handleUnauthorizedError(w, err, log)
		return
	}
	routes, err := h.db.ReadRoutes()
	if err != nil {
		handleDBCommunicationError(w, err, log)
		return
	}
	encoder := json.NewEncoder(w)
	encoder.Encode(routes)
}

func (h *RoutesHandler) Upsert(w http.ResponseWriter, req *http.Request) {
	log := h.logger.Session("create-route")
	decoder := json.NewDecoder(req.Body)

	var routes []db.Route
	err := decoder.Decode(&routes)
	if err != nil {
		handleProcessRequestError(w, err, log)
		return
	}

	log.Info("request", lager.Data{"route_creation": routes})

	err = h.token.DecodeToken(req.Header.Get("Authorization"), RoutingRoutesWriteScope)
	if err != nil {
		handleUnauthorizedError(w, err, log)
		return
	}

	apiErr := h.validator.ValidateCreate(routes, h.maxTTL)
	if apiErr != nil {
		handleApiError(w, apiErr, log)
		return
	}

	for _, route := range routes {
		err = h.db.SaveRoute(route)
		if err != nil {
			handleDBCommunicationError(w, err, log)
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *RoutesHandler) Delete(w http.ResponseWriter, req *http.Request) {
	log := h.logger.Session("delete-route")
	decoder := json.NewDecoder(req.Body)

	var routes []db.Route
	err := decoder.Decode(&routes)
	if err != nil {
		handleProcessRequestError(w, err, log)
		return
	}

	log.Info("request", lager.Data{"route_deletion": routes})

	err = h.token.DecodeToken(req.Header.Get("Authorization"), RoutingRoutesWriteScope)
	if err != nil {
		handleUnauthorizedError(w, err, log)
		return
	}

	apiErr := h.validator.ValidateDelete(routes)
	if apiErr != nil {
		handleApiError(w, apiErr, log)
		return
	}

	for _, route := range routes {
		err = h.db.DeleteRoute(route)
		if err != nil {
			if dberr, ok := err.(db.DBError); !ok || dberr.Type != db.KeyNotFound {
				handleDBCommunicationError(w, err, log)
				return
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
