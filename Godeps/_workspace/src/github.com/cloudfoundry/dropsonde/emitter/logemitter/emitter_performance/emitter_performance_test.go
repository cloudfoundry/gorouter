package emitter_performance

import (
	"github.com/cloudfoundry/dropsonde/emitter/logemitter"
	"github.com/cloudfoundry/loggregatorlib/loggregatorclient/fake"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	SECOND = float64(1 * time.Second)
)

type messageFixture struct {
	name                string
	message             string
	logMessageExpected  float64
	logEnvelopeExpected float64
}

func (mf *messageFixture) getExpected(isEnvelope bool) float64 {
	if isEnvelope {
		return mf.logEnvelopeExpected
	}
	return mf.logMessageExpected
}

var messageFixtures = []*messageFixture{
	{"long message", longMessage(), 1 * SECOND, 2 * SECOND},
	{"message with newlines", messageWithNewlines(), 3 * SECOND, 5 * SECOND},
	{"message worst case", longMessage() + "\n", 1 * SECOND, 1 * SECOND},
}

func longMessage() string {
	return strings.Repeat("a", logemitter.MAX_MESSAGE_BYTE_SIZE*2)
}

func messageWithNewlines() string {
	return strings.Repeat(strings.Repeat("a", 6*1024)+"\n", 10)
}

func BenchmarkLogEnvelopeEmit(b *testing.B) {
	log.SetOutput(ioutil.Discard)
	received := make(chan *[]byte, 1)
	os.Setenv("LOGGREGATOR_SHARED_SECRET", "secret")
	e, _ := logemitter.NewEmitter("localhost:3457", "ROUTER", "42", false)
	e.LoggregatorClient = &fake.FakeLoggregatorClient{Received: received}

	testEmitHelper(b, e, received, true)
}

func testEmitHelper(b *testing.B, e logemitter.Emitter, received chan *[]byte, isEnvelope bool) {
	go func() {
		for {
			<-received
		}
	}()

	for _, fixture := range messageFixtures {
		startTime := time.Now().UnixNano()

		for i := 0; i < b.N; i++ {
			e.Emit("appid", fixture.message)
		}
		elapsedTime := float64(time.Now().UnixNano() - startTime)

		expected := fixture.getExpected(isEnvelope)
		if elapsedTime > expected {
			b.Errorf("Elapsed time for %s should have been below %vs, but was %vs", fixture.name, expected/SECOND, float64(elapsedTime)/SECOND)
		}
	}
}
