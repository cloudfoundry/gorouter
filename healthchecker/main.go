package main

import (
	"flag"

	"code.cloudfoundry.org/gorouter/healthchecker/watchdog"
)

func main() {
	flag.StringVar(&configFile, "c", "", "Configuration File")
	flag.Parse()

	// bind syscall.SIGUSR

	// configFile.read
	address := "http://"

	w := watchdog.NewWatchdog()
	_ = w.HitHealthcheckEndpoint()

	// give enough retries or a grace period to not interfere with the
	// desired gorouter behavior

	// followup story about gorouter hanging
	// gorouter :80       status :8080        healthchecker
	// healthy            healthy             200 -> no exit
	// hangs              not health
	// bad state

	// responds           not healthy
}
