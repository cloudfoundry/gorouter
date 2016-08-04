package common

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os/user"

	"code.cloudfoundry.org/gorouter/common/secure"
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

	key = bytes.Trim(key, "\n")
	secretPbkdf := secure.NewPbkdf2(key, 16)
	crypto, err := secure.NewAesGCM(secretPbkdf)
	if err != nil {
		fmt.Printf("Error creating crypto: %s\n", err)
		return nil, err
	}
	return crypto, nil
}
