package common

import (
	steno "github.com/cloudfoundry/gosteno"
	"github.com/nu7hatch/gouuid"
)

var log = steno.NewLogger("common.logger")

func GenerateUUID() (string, error) {
	uuid, err := uuid.NewV4()
	if err != nil {
		return "", err
	}
	return uuid.String(), nil
}
