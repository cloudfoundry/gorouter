package uuid

import . "github.com/nu7hatch/gouuid"

func GenerateUUID() (string, error) {
	guid, err := NewV4()
	if err != nil {
		return "", err
	}
	return guid.String(), nil
}
