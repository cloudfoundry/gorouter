package main

import (
	"flag"
	"router"
)

var configFile string

func init() {
	flag.StringVar(&configFile, "c", "config/router.yml", "Configuration File")

	flag.Parse()
}

func main() {
	router.InitConfigFromFile(configFile)
	router.SetupLogger()

	router.NewRouter().Run()
}
