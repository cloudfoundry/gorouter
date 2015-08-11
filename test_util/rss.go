package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cloudfoundry/gorouter/common/secure"
	"github.com/cloudfoundry/gorouter/route_service"
	"github.com/codegangsta/cli"
)

var genFlags = []cli.Flag{
	cli.StringFlag{
		Name:  "time, t",
		Usage: "Timestamp the signature",
	},
	cli.StringFlag{
		Name:  "url, u",
		Usage: "Client url (required)",
	},
}

var readFlags = []cli.Flag{
	cli.StringFlag{
		Name:  "signature, s",
		Usage: "Route service signature, base64 encoded (Required)",
	},
	cli.StringFlag{
		Name:  "metadata, m",
		Usage: "Route service metadata, base64 encoded (Required)",
	},
	cli.StringFlag{
		Name:  "key, k",
		Usage: "Key used to decrypt a route service signature",
	},
}

var cliCommands = []cli.Command{
	{
		Name:        "generate",
		Usage:       "Generates a Route Service Signature",
		Aliases:     []string{"g"},
		Description: "Generates a Route Service Signature with the current time",
		Action:      generateSignature,
		Flags:       genFlags,
	},
	{
		Name:    "read",
		Usage:   "Decodes and decrypts a route service signature",
		Aliases: []string{"r", "o"},
		Description: `Decodes and decrypts a route service signature using the key file:
key can be passed in as an argument`,
		Action: readSignature,
		Flags:  readFlags,
	},
}

func main() {
	fmt.Println()
	app := cli.NewApp()
	app.Name = "rss"
	app.Usage = "A CLI for the generating and opening a route service signature."
	authors := []cli.Author{cli.Author{Name: "Cloud Foundry Routing Team", Email: "cf-dev@lists.cloudfoundry.org"}}
	app.Authors = authors
	app.Commands = cliCommands
	app.CommandNotFound = commandNotFound
	app.Version = "0.1.0"

	app.Run(os.Args)
	os.Exit(0)
}

func readSignature(c *cli.Context) {
	crypto, err := createCrypto(c)
	if err != nil {
		os.Exit(1)
	}

	sigEncoded := c.String("signature")
	metaEncoded := c.String("metadata")

	if sigEncoded == "" || metaEncoded == "" {
		cli.ShowCommandHelp(c, "read")
		os.Exit(1)
	}

	signature, err := route_service.SignatureFromHeaders(sigEncoded, metaEncoded, crypto)

	if err != nil {
		fmt.Printf("Failed to read signature: %s", err.Error())
		os.Exit(1)
	}

	printSignature(signature)
}

func printSignature(signature route_service.Signature) {
	signatureJson, _ := json.MarshalIndent(&signature, "", "  ")
	fmt.Printf("Decoded Signature:\n%s\n\n", signatureJson)
}

func generateSignature(c *cli.Context) {
	crypto, err := createCrypto(c)
	if err != nil {
		os.Exit(1)
	}

	signature, err := createSigFromArgs(c)
	if err != nil {
		os.Exit(1)
	}

	sigEncoded, metaEncoded, err := route_service.BuildSignatureAndMetadata(crypto, &signature)
	if err != nil {
		fmt.Printf("Failed to create signature: %s", err.Error())
		os.Exit(1)
	}

	fmt.Printf("Encoded Signature:\n%s\n\n", sigEncoded)
	fmt.Printf("Encoded Metadata:\n%s\n\n", metaEncoded)
}

func createSigFromArgs(c *cli.Context) (route_service.Signature, error) {
	signature := route_service.Signature{}
	url := c.String("url")

	if url == "" {
		cli.ShowCommandHelp(c, "generate")
		return signature, errors.New("url is required")
	}

	var sigTime time.Time

	timeStr := c.String("time")

	if timeStr != "" {
		unix, err := strconv.ParseInt(timeStr, 10, 64)
		if err != nil {
			fmt.Printf("Invalid time format: %s", timeStr)
			return signature, err
		}

		sigTime = time.Unix(unix, 0)
	} else {
		sigTime = time.Now()
	}

	return route_service.Signature{
		RequestedTime: sigTime,
		ForwardedUrl:  url,
	}, nil
}

func createCrypto(c *cli.Context) (*secure.AesGCM, error) {
	key := c.String("key")

	if key == "" {
		// TODO: load it from a file $PWD/key
		key = "8kvkdHTEPDjV+CzX6UPtnWilLwwnBkViScOykQmpgkw="
	}

	secretDecoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		fmt.Printf("Error decoding key: %s\n", err)
		return nil, err
	}

	crypto, err := secure.NewAesGCM(secretDecoded)
	if err != nil {
		fmt.Printf("Error creating crypto: %s\n", err)
		return nil, err
	}
	return crypto, nil
}

func checkArguments(c *cli.Context, cmd string) []string {
	var issues []string

	switch cmd {
	case "register", "unregister":
		if len(c.Args()) > 1 {
			issues = append(issues, "Unexpected arguments.")
		} else if len(c.Args()) < 1 {
			issues = append(issues, "Must provide routes JSON.")
		}
	case "list":
		if len(c.Args()) > 0 {
			issues = append(issues, "Unexpected arguments.")
		}
	}

	return issues
}

func commandNotFound(c *cli.Context, cmd string) {
	fmt.Println("Not a valid command:", cmd)
	os.Exit(1)
}
