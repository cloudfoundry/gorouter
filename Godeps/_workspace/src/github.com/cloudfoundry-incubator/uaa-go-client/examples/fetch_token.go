package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	client "github.com/cloudfoundry-incubator/uaa-go-client"
	"github.com/cloudfoundry-incubator/uaa-go-client/config"
	"github.com/cloudfoundry-incubator/uaa-go-client/schema"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
)

func main() {
	var (
		err       error
		uaaClient client.Client
		token     *schema.Token
	)

	if len(os.Args) < 5 {
		fmt.Printf("Usage: <client-name> <client-secret> <uaa-url> <skip-verification>\n\n")
		fmt.Printf("For example: client-name client-secret https://uaa.service.cf.internal:8443 true\n")
		return
	}

	skip, err := strconv.ParseBool(os.Args[4])
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	cfg := &config.Config{
		ClientName:       os.Args[1],
		ClientSecret:     os.Args[2],
		UaaEndpoint:      os.Args[3],
		SkipVerification: skip,
	}

	logger := lager.NewLogger("test")
	clock := clock.NewClock()

	uaaClient, err = client.NewClient(logger, cfg, clock)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	fmt.Printf("Connecting to: %s ...\n", cfg.UaaEndpoint)

	token, err = uaaClient.FetchToken(true)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	fmt.Printf("Response:\n\ttoken: %s\n\texpires: %d\n", token.AccessToken, token.ExpiresIn)

}
