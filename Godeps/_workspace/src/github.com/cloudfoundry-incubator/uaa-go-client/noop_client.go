package uaa_go_client

import "github.com/cloudfoundry-incubator/uaa-go-client/schema"

type NoOpUaaClient struct {
}

func NewNoOpUaaClient() Client {
	return &NoOpUaaClient{}
}

func (c *NoOpUaaClient) FetchToken(useCachedToken bool) (*schema.Token, error) {
	return &schema.Token{}, nil
}
func (c *NoOpUaaClient) DecodeToken(uaaToken string, desiredPermissions ...string) error {
	return nil
}
func (c *NoOpUaaClient) FetchKey() (string, error) {
	return "", nil
}
