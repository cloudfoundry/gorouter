package common

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os/user"

	"github.com/cloudfoundry/gorouter/common/secure"
	"github.com/codegangsta/cli"
)

func CreateCrypto(c *cli.Context) (*secure.AesGCM, error) {
	keyPath := c.String("key-path")

	if keyPath == "" {
		usr, err := user.Current()
		if err != nil {
			fmt.Println(err.Error())
		}
		keyPath = usr.HomeDir + "/.rss/key"
	}

	key, err := ioutil.ReadFile(keyPath)
	if err != nil {
		fmt.Printf("Unable to read key file: %s\n%s\n", keyPath, err.Error())
		return nil, err
	}

	secretDecoded, err := base64.StdEncoding.DecodeString(string(key))
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
