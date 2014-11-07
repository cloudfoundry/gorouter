package emitter

import "github.com/cloudfoundry/dropsonde/control"

type RespondingByteEmitter interface {
	ByteEmitter
	Respond(*control.ControlMessage)
}
