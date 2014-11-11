# Dropsonde

[![Build Status](https://travis-ci.org/cloudfoundry/dropsonde.svg?branch=master)](https://travis-ci.org/cloudfoundry/dropsonde) [![Coverage Status](https://img.shields.io/coveralls/cloudfoundry/dropsonde.svg)](https://coveralls.io/r/cloudfoundry/dropsonde?branch=master)
[![GoDoc](https://godoc.org/github.com/cloudfoundry/dropsonde?status.png)](https://godoc.org/github.com/cloudfoundry/dropsonde)

Go library to collect and emit metric and logging data from CF components.
https://godoc.org/github.com/cloudfoundry/dropsonde
## Protocol Buffer format
See [dropsonde-protocol](http://www.github.com/cloudfoundry/dropsonde-protocol)
for the full specification of the dropsonde Protocol Buffer format.

Use [this script](events/generate-events.sh) to generate Go handlers for the
various protobuf messages.

## Initialization and Configuration
```go
import (
    _ "github.com/cloudfoundry/dropsonde"
)

func main() {
    dropsonde.Initialize("localhost:3457", "router", "z1", "0")
}
```
This initializes dropsonde, along with the logs and metrics packages. It also instruments
the default HTTP handler for outgoing requests, instrument itself (to count messages sent, etc.), 
and provides basic [runtime stats](runtime_stats/runtime_stats.go).

The first argument is the destination for messages (typically metron).
The host and port is required. The remaining arguments form the origin.
This list is used by downstream portions of the dropsonde system to
track the source of metrics.

Alternatively, import `github.com/cloudfoundry/dropsonde/metrics` to include the
ability to send custom metrics, via [`metrics.SendValue`](metrics/metrics.go#L44)
and [`metrics.IncrementCounter`](metrics/metrics.go#L51).

## Manual usage
For details on manual usage of dropsonde, please refer to the
[Godocs](https://godoc.org/github.com/cloudfoundry/dropsonde). Pay particular
attenion to the `ByteEmitter`, `InstrumentedHandler`, and `InstrumentedRoundTripper`
types.

## Handling dropsonde events
Programs wishing to emit events and metrics should use the package as described
above. For programs that wish to process events, we provide the `dropsonde/unmarshaller`
and `dropsonde/marshaller` packages for decoding/reencoding raw Protocol Buffer
messages. Use [`dropsonde/signature`](signature/signature_verifier.go) to sign
and validate messages.
