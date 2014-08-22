package fake

import "github.com/cloudfoundry/loggregatorlib/cfcomponent/instrumentation"

type FakeLoggregatorClient struct {
	Received chan *[]byte
}

func (flc FakeLoggregatorClient) Send(data []byte) {
	flc.Received <- &data
}

func (flc FakeLoggregatorClient) Emit() instrumentation.Context {
	return instrumentation.Context{}
}

func (flc FakeLoggregatorClient) Stop() {}
