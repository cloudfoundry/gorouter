// Package dropsonde provides sensible defaults for using dropsonde.
//
// The default HTTP transport is instrumented, as well as some basic stats about
// the Go runtime. Additionally, the default emitter is itself instrumented to
// periodically send "heartbeat" messages containing counts of received and sent
// events. The default emitter sends events over UDP.
//
// Use
//
// dropsonde.Initialize("localhost:3457", origins...)
//
// to initialize. See package metrics and logs for other usage.

package dropsonde

import (
	"errors"
	"fmt"
	"github.com/cloudfoundry/dropsonde/emitter"
	"github.com/cloudfoundry/dropsonde/events"
	"github.com/cloudfoundry/dropsonde/instrumented_handler"
	"github.com/cloudfoundry/dropsonde/instrumented_round_tripper"
	"github.com/cloudfoundry/dropsonde/log_sender"
	"github.com/cloudfoundry/dropsonde/logs"
	"github.com/cloudfoundry/dropsonde/metric_sender"
	"github.com/cloudfoundry/dropsonde/metrics"
	"github.com/cloudfoundry/dropsonde/runtime_stats"
	"github.com/cloudfoundry/gosteno"
	"net/http"
	"strings"
	"time"
)

var autowiredEmitter emitter.EventEmitter

const (
	runtimeStatsInterval = 10 * time.Second
	originDelimiter      = "/"
)

func init(){
	autowiredEmitter = &NullEventEmitter{}
}

// Initialize creates default emitters and instruments the default HTTP
// transport.
//
// The origin variable is required and specifies the
// source name for all metrics emitted by this process. If it is not set, the
// program will run normally but will not emit metrics.
//
// The destination variable sets the host and port to
// which metrics are sent. It is optional, and defaults to DefaultDestination.
func Initialize(destination string, origin ...string) error {
	autowiredEmitter = nil
	emitter, err := createDefaultEmitter(strings.Join(origin, originDelimiter), destination)
	if err != nil {
		return err
	}

	autowiredEmitter = emitter
	initialize()

	return nil
}

func InitializeWithEmitter(emitter emitter.EventEmitter) {
	autowiredEmitter = emitter
	initialize()
}

func AutowiredEmitter() emitter.EventEmitter {
	return autowiredEmitter
}

// InstrumentedHandler returns a Handler pre-configured to emit HTTP server
// request metrics to AutowiredEmitter.
func InstrumentedHandler(handler http.Handler) http.Handler {
	return instrumented_handler.InstrumentedHandler(handler, autowiredEmitter)
}

// InstrumentedRoundTripper returns a RoundTripper pre-configured to emit
// HTTP client request metrics to AutowiredEmitter.
func InstrumentedRoundTripper(roundTripper http.RoundTripper) http.RoundTripper {
	return instrumented_round_tripper.InstrumentedRoundTripper(roundTripper, autowiredEmitter)
}

func initialize() {
	metrics.Initialize(metric_sender.NewMetricSender(AutowiredEmitter()))
	logs.Initialize(log_sender.NewLogSender(AutowiredEmitter(), gosteno.NewLogger("dropsonde/logs")))
	go runtime_stats.NewRuntimeStats(autowiredEmitter, runtimeStatsInterval).Run(nil)
	http.DefaultTransport = InstrumentedRoundTripper(http.DefaultTransport)
}

func createDefaultEmitter(origin, destination string) (emitter.EventEmitter, error) {
	if len(origin) == 0 {
		return nil, errors.New("Failed to initialize dropsonde: origin variable not set")
	}

	if len(destination) == 0 {
		return nil, errors.New("Failed to initialize dropsonde: destination variable not set")
	}

	udpEmitter, err := emitter.NewUdpEmitter(destination)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to initialize dropsonde: %v", err.Error()))
	}

	heartbeatResponder, err := emitter.NewHeartbeatResponder(udpEmitter, origin)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to initialize dropsonde: %v", err.Error()))
	}

	go udpEmitter.ListenForHeartbeatRequest(heartbeatResponder.Respond)

	return emitter.NewEventEmitter(heartbeatResponder, origin), nil
}

type NullEventEmitter struct{}

func (*NullEventEmitter) Emit(events.Event) error {
	return nil
}

func (*NullEventEmitter) Close() {}
