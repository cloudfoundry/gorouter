package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util/rss/common"
	"github.com/urfave/cli"
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

	signatureContents, err := routeservice.SignatureContentsFromHeaders(sigEncoded, metaEncoded, crypto)

	if err != nil {
		fmt.Printf("Failed to read signature: %s\n", err.Error())
		os.Exit(1)
	}

	printSignatureContents(signatureContents)
}

func printSignatureContents(signatureContents routeservice.SignatureContents) {
	signatureJson, _ := json.MarshalIndent(&signatureContents, "", "  ")
	fmt.Printf("Decoded Signature:\n%s\n\n", signatureJson)
}
