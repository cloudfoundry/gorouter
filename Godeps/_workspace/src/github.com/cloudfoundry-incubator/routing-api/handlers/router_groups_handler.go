package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/cloudfoundry-incubator/routing-api/db"
	uaaclient "github.com/cloudfoundry-incubator/uaa-go-client"
	"github.com/pivotal-golang/lager"
)

type RouterGroupsHandler struct {
	uaaClient uaaclient.Client
	logger    lager.Logger
	db        db.DB
}

func NewRouteGroupsHandler(uaaClient uaaclient.Client, logger lager.Logger, db db.DB) *RouterGroupsHandler {
	return &RouterGroupsHandler{
		uaaClient: uaaClient,
		logger:    logger,
		db:        db,
	}
}

func (h *RouterGroupsHandler) ListRouterGroups(w http.ResponseWriter, req *http.Request) {
	log := h.logger.Session("list-router-groups")
	log.Debug("started")
	defer log.Debug("completed")

	err := h.uaaClient.DecodeToken(req.Header.Get("Authorization"), RouterGroupsReadScope)
	if err != nil {
		handleUnauthorizedError(w, err, log)
		return
	}

	routerGroups, err := h.db.ReadRouterGroups()
	if err != nil {
		handleDBCommunicationError(w, err, log)
	}

	jsonBytes, err := json.Marshal(routerGroups)
	if err != nil {
		log.Error("failed-to-marshal", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonBytes)
	w.Header().Set("Content-Length", strconv.Itoa(len(jsonBytes)))
}
