package main

import (
	"flag"
	"router"
)

var configFile string

func init() {
	flag.StringVar(&configFile, "c", "config/router.json", "Configuration File")

	flag.Parse()
}

func main() {
	router.GenerateConfig(configFile)

	r := router.NewRouter()

	r.Run()
}
