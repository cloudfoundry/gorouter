package main

import "watchdog"

func main() {
	w := watchdog.NewWatchdog()
	err := hitHealthcheckEndpoint()
}
