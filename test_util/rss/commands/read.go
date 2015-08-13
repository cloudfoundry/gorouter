package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/cloudfoundry/gorouter/route_service"
	"github.com/cloudfoundry/gorouter/test_util/rss/common"
	"github.com/codegangsta/cli"
)

func ReadSignature(c *cli.Context) {
	sigEncoded := c.String("signature")
	metaEncoded := c.String("metadata")

	if sigEncoded == "" || metaEncoded == "" {
		cli.ShowCommandHelp(c, "read")
		os.Exit(1)
	}

	crypto, err := common.CreateCrypto(c)
	if err != nil {
		os.Exit(1)
	}

	signature, err := route_service.SignatureFromHeaders(sigEncoded, metaEncoded, crypto)

	if err != nil {
		fmt.Printf("Failed to read signature: %s\n", err.Error())
		os.Exit(1)
	}

	printSignature(signature)
}

func printSignature(signature route_service.Signature) {
	signatureJson, _ := json.MarshalIndent(&signature, "", "  ")
	fmt.Printf("Decoded Signature:\n%s\n\n", signatureJson)
}
