package helpers

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry-incubator/routing-api/models"
	"github.com/pivotal-golang/lager"
)

type RouteRegister struct {
	database db.DB
	route    models.Route
	ticker   *time.Ticker
	logger   lager.Logger
}

func NewRouteRegister(database db.DB, route models.Route, ticker *time.Ticker, logger lager.Logger) *RouteRegister {
	return &RouteRegister{
		database: database,
		route:    route,
		ticker:   ticker,
		logger:   logger,
	}
}

func (r *RouteRegister) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	err := r.database.SaveRoute(r.route)
	if err != nil {
		r.logger.Error("registration-error", err)
	}
	close(ready)

	for {

		select {
		case <-r.ticker.C:
			err = r.database.SaveRoute(r.route)
		case <-signals:
			err := r.database.DeleteRoute(r.route)
			if err != nil {
				r.logger.Error("unregistration-error", err)
				return err
			}
			return nil
		}
	}
}
