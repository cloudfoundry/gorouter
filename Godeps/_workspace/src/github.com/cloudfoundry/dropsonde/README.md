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

## Autowire
The intended use of dropsonde is through the [autowire](autowire/autowire.go)
sub-package. Simply anonymously import the package
```go
import (
    _ "github.com/cloudfoundry/dropsonde/autowire"
)
```
and it will automatically initialize, instrument the default HTTP handler for
outgoing requests, instrument itself (to count messages sent, etc.), and provide
basic [runtime stats](runtime_stats/runtime_stats.go).

Alternatively, import `github.com/cloudfoundry/dropsonde/autowire/metrics` to include the
ability to send custom metrics, via [`metrics.SendValue`](autowire/metrics/metrics.go#L44)
and [`metrics.IncrementCounter`](autowire/metrics/metrics.go#L51). (The same auto-
initialization will apply when importing `metrics`.)

### Configuration
Before running a program using `autowire`, you **must** set the `DROPSONDE_ORIGIN`
environment variable. This string is used by downstream portions of the dropsonde
system to track the source of metrics. Failing to set this variable will result
in the program running, but without any instrumentation.

You may (optionally) set `DROPSONDE_DESTINATION` to configure the recipient of
event messages. If left unset, a [default](autowire/autowire.go#L37) will be
used.

## Manual usage
For details on manual usage of dropsonde, please refer to the
[Godocs](https://godoc.org/github.com/cloudfoundry/dropsonde). Pay particular
attenion to the `ByteEmitter`, `InstrumentedHandler`, and `InstrumentedRoundTripper`
types.

## Handling dropsonde events
Programs wishing to emit events and metrics should use the package as described
above, or should use `dropsonde/autowire` (or `dropsonde/autowire/metrics`). For
programs that wish to process events, we provide the `dropsonde/unmarshaller`
and `dropsonde/marshaller` packages for decoding/reencoding raw Protocol Buffer
messages. Use [`dropsonde/signature`](signature/signature_verifier.go) to sign
and validate messages.
