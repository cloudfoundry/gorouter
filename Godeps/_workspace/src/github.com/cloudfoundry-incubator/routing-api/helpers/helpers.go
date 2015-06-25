package helpers

import (
	"os"
	"time"

	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/pivotal-golang/lager"
)

type RouteRegister struct {
	database db.DB
	route    db.Route
	ticker   *time.Ticker
	logger   lager.Logger
}

func NewRouteRegister(database db.DB, route db.Route, ticker *time.Ticker, logger lager.Logger) *RouteRegister {
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
		r.logger.Error("Error registering self", err)
	}
	close(ready)

	for {

		select {
		case <-r.ticker.C:
			err = r.database.SaveRoute(r.route)
		case <-signals:
			err := r.database.DeleteRoute(r.route)
			if err != nil {
				r.logger.Error("Error deleting route registration", err)
				return err
			}
			return nil
		}
	}
}
