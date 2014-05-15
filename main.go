package main

import (
	"github.com/cloudfoundry/gorouter/config"
	"github.com/cloudfoundry/gorouter/log"
	rregistry "github.com/cloudfoundry/gorouter/registry"
	"github.com/cloudfoundry/gorouter/router"
	rvarz "github.com/cloudfoundry/gorouter/varz"
	"github.com/cloudfoundry/yagnats"

	"flag"
	"os"
)

var configFile string

func init() {
	flag.StringVar(&configFile, "c", "", "Configuration File")

	flag.Parse()
}

func main() {
	c := config.DefaultConfig()
	if configFile != "" {
		c = config.InitConfigFromFile(configFile)
	}

	log.SetupLoggerFromConfig(c)

	mbus := yagnats.NewClient()
	registry := rregistry.NewCFRegistry(c, mbus)
	varz := rvarz.NewVarz(registry)
	router, err := router.NewRouter(c, mbus, registry, varz)
	if err != nil {
		log.Errorf("An error occurred: %s", err.Error())
		os.Exit(1)
	}

	errChan := router.Run()

	err = <-errChan
	if err != nil {
		log.Errorf("Error occurred:", err.Error())
		os.Exit(1)
	}

	os.Exit(0)
}
