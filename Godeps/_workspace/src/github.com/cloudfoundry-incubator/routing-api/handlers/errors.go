package handlers

import (
	"encoding/json"
	routing_api "github.com/cloudfoundry-incubator/routing-api"
	"github.com/cloudfoundry-incubator/routing-api/metrics"
	"github.com/pivotal-golang/lager"
	"net/http"
)

func handleProcessRequestError(w http.ResponseWriter, procErr error, log lager.Logger) {
	log.Error("error", procErr)

	retErr, _ := json.Marshal(routing_api.NewError(routing_api.ProcessRequestError, "Cannot process request: "+procErr.Error()))

	w.WriteHeader(http.StatusBadRequest)
	w.Write(retErr)
}

func handleApiError(w http.ResponseWriter, apiErr *routing_api.Error, log lager.Logger) {
	log.Error("error", apiErr)

	retErr, _ := json.Marshal(apiErr)

	w.WriteHeader(http.StatusBadRequest)
	w.Write(retErr)
}

func handleDBCommunicationError(w http.ResponseWriter, err error, log lager.Logger) {
	log.Error("error", err)

	retErr, _ := json.Marshal(routing_api.NewError(routing_api.DBCommunicationError, err.Error()))

	w.WriteHeader(http.StatusInternalServerError)
	w.Write(retErr)
}

func handleUnauthorizedError(w http.ResponseWriter, err error, log lager.Logger) {
	log.Error("error", err)
	metrics.IncrementTokenError()
	retErr, _ := json.Marshal(routing_api.NewError(routing_api.UnauthorizedError, err.Error()))

	w.WriteHeader(http.StatusUnauthorized)
	w.Write(retErr)
}
