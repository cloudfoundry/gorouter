package main

import (
	"fmt"
)

func main() {
	fmt.Println("hi")
	// w := watchdog.NewWatchdog()
	// _ = w.HitHealthcheckEndpoint()
	msg := make(chan string)

	go func() {
		msg <- "yo"
	}()

	fmt.Println(<-msg)
}
