package fakeyagnats

import (
	"testing"

	"github.com/cloudfoundry/yagnats"
)

func FunctionTakingNATSClient(yagnats.NATSClient) {

}

func FunctionTakingApceraClientWrapper(yagnats.ApceraWrapperNATSClient) {

}

func TestCanPassFakeYagnatsAsNATSClient(t *testing.T) {
	FunctionTakingNATSClient(New())
}

func TestCanPassFakeYagnatsAsApceraClientWrapper(t *testing.T) {
	FunctionTakingApceraClientWrapper(NewApceraClientWrapper())
}
