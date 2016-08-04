package main

import (
	"fmt"
	"os"

	"code.cloudfoundry.org/gorouter/test_util/rss/commands"
	"github.com/codegangsta/cli"
)

var keyFlag = cli.StringFlag{
	Name:  "key-path, p, k",
	Usage: "Path of the key file used to decrypt a route service signature",
}

var timeFlag = cli.StringFlag{
	Name:  "time, t",
	Usage: "Timestamp the signature",
}

var urlFlag = cli.StringFlag{
	Name:  "url, u",
	Usage: "Client url (required)",
}

var signatureFlag = cli.StringFlag{
	Name:  "signature, s",
	Usage: "Route service signature, base64 encoded (Required)",
}

var metadataFlag = cli.StringFlag{
	Name:  "metadata, m",
	Usage: "Route service metadata, base64 encoded (Required)",
}

var genFlags = []cli.Flag{urlFlag, timeFlag, keyFlag}

var readFlags = []cli.Flag{signatureFlag, metadataFlag, keyFlag}

var cliCommands = []cli.Command{
	{
		Name:        "generate",
		Usage:       "Generates a Route Service Signature",
		Aliases:     []string{"g"},
		Description: "Generates a Route Service Signature with the current time",
		Action:      commands.GenerateSignature,
		Flags:       genFlags,
	},
	{
		Name:    "read",
		Usage:   "Decodes and decrypts a route service signature",
		Aliases: []string{"r", "o"},
		Description: `Decodes and decrypts a route service signature using the key file:
key can be passed in as an argument`,
		Action: commands.ReadSignature,
		Flags:  readFlags,
	},
}

func main() {
	fmt.Println()
	app := cli.NewApp()
	app.Name = "rss"
	app.Usage = "A CLI for generating and opening a route service signature."
	authors := []cli.Author{cli.Author{Name: "Cloud Foundry Routing Team", Email: "cf-dev@lists.cloudfoundry.org"}}
	app.Authors = authors
	app.Commands = cliCommands
	app.CommandNotFound = commandNotFound
	app.Version = "0.1.0"

	app.Run(os.Args)
	os.Exit(0)
}

func commandNotFound(c *cli.Context, cmd string) {
	fmt.Println("Not a valid command:", cmd)
	os.Exit(1)
}
