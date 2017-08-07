package commands

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"code.cloudfoundry.org/gorouter/routeservice"
	"code.cloudfoundry.org/gorouter/test_util/rss/common"
	"github.com/codegangsta/cli"
)

func GenerateSignature(c *cli.Context) {
	url := c.String("url")

	if url == "" {
		cli.ShowCommandHelp(c, "generate")
		os.Exit(1)
	}

	crypto, err := common.CreateCrypto(c)
	if err != nil {
		os.Exit(1)
	}

	signature, err := createSigFromArgs(c)
	if err != nil {
		os.Exit(1)
	}

	sigEncoded, metaEncoded, err := routeservice.BuildSignatureAndMetadata(crypto, &signature)
	if err != nil {
		fmt.Printf("Failed to create signature: %s", err.Error())
		os.Exit(1)
	}

	fmt.Printf("Encoded Signature:\n%s\n\n", sigEncoded)
	fmt.Printf("Encoded Metadata:\n%s\n\n", metaEncoded)
}

func createSigFromArgs(c *cli.Context) (routeservice.Signature, error) {
	signature := routeservice.Signature{}
	url := c.String("url")

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

	return routeservice.Signature{
		RequestedTime: sigTime,
		ForwardedUrl:  url,
	}, nil
}
