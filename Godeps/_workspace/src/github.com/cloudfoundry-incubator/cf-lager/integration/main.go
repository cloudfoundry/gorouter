package main

import (
	"errors"
	"flag"

	"github.com/cloudfoundry-incubator/cf-lager"
	"github.com/pivotal-golang/lager"
)

func main() {
	cf_lager.AddFlags(flag.CommandLine)
	flag.Parse()

	logger, _ := cf_lager.New("cf-lager-integration")

	logger.Debug("component-does-action", lager.Data{"debug-detail": "foo"})
	logger.Info("another-component-action", lager.Data{"info-detail": "bar"})
	logger.Error("component-failed-something", errors.New("error"), lager.Data{"error-detail": "baz"})
	logger.Fatal("component-failed-badly", errors.New("fatal"), lager.Data{"fatal-detail": "quux"})
}
