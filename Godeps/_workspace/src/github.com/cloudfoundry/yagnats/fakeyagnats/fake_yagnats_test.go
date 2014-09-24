package fakeyagnats

import (
	"testing"

	"github.com/cloudfoundry/yagnats"
)

func FunctionTakingNATSClient(yagnats.NATSClient) {

}

func FunctionTakingNatsConn(yagnats.NATSConn) {

}

func TestCanPassFakeYagnatsAsNATSClient(t *testing.T) {
	FunctionTakingNATSClient(New())
}

func TestCanPassFakeYagnatsAsNatsDotConn(t *testing.T) {
	FunctionTakingNatsConn(Connect())
}
