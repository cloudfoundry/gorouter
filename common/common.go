package common

import (
	"github.com/nu7hatch/gouuid"
	"github.com/pivotal-golang/lager"
)

var log lager.Logger

func GenerateUUID() (string, error) {
	uuid, err := uuid.NewV4()
	if err != nil {
		return "", err
	}
	return uuid.String(), nil
}
