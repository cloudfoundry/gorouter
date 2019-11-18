package handlers

import (
	"encoding/hex"
	"fmt"
	"strings"

	"code.cloudfoundry.org/gorouter/common/secure"
)

const (
	W3CTraceparentVersion    = uint8(0)
	W3CTraceparentSampled    = uint8(1)
	W3CTraceparentNotSampled = uint8(0)
)

// W3CTraceparent is a struct which represents the traceparent header
// See https://www.w3.org/TR/trace-context/
type W3CTraceparent struct {
	Version  uint8
	TraceID  []byte
	ParentID []byte
	Flags    uint8
}

// NewW3CTraceparent generates a W3C traceparent header value according to
// https://www.w3.org/TR/trace-context/#version-format
func NewW3CTraceparent() (W3CTraceparent, error) {
	traceID, err := generateW3CTraceID()
	if err != nil {
		return W3CTraceparent{}, err
	}

	parentID, err := generateW3CParentID()
	if err != nil {
		return W3CTraceparent{}, err
	}

	return W3CTraceparent{
		Version: W3CTraceparentVersion,
		Flags:   W3CTraceparentSampled,

		TraceID:  traceID,
		ParentID: parentID,
	}, nil
}

// ParseW3CTraceparent parses a W3C traceparent header value according to
// https://www.w3.org/TR/trace-context/#version-format
// If it cannot parse the input header string it returns nil
func ParseW3CTraceparent(header string) *W3CTraceparent {
	// In the format of
	// 00-00000000000000000000000000000000-0000000000000000-00
	sanitizedHeader := strings.TrimSpace(strings.ToLower(header))

	if len(sanitizedHeader) != 55 {
		return nil
	}

	versionBytes, err := hex.DecodeString(sanitizedHeader[0:2])
	if err != nil {
		return nil
	}

	traceIDBytes, err := hex.DecodeString(sanitizedHeader[3:35])
	if err != nil {
		return nil
	}

	parentIDBytes, err := hex.DecodeString(sanitizedHeader[36:52])
	if err != nil {
		return nil
	}

	flagBytes, err := hex.DecodeString(sanitizedHeader[53:55])
	if err != nil {
		return nil
	}

	return &W3CTraceparent{
		Version: uint8(versionBytes[0]),
		Flags:   uint8(flagBytes[0]),

		TraceID:  traceIDBytes,
		ParentID: parentIDBytes,
	}
}

func generateW3CTraceID() ([]byte, error) {
	randBytes, err := secure.RandomBytes(16)
	if err != nil {
		return []byte{}, err
	}

	return randBytes, nil
}

func generateW3CParentID() ([]byte, error) {
	randBytes, err := secure.RandomBytes(8)
	if err != nil {
		return []byte{}, err
	}

	return randBytes, nil
}

// Next generates a new Traceparent
func (h W3CTraceparent) Next() (W3CTraceparent, error) {
	parentID, err := generateW3CParentID()

	if err != nil {
		return h, err
	}

	return W3CTraceparent{
		Version:  W3CTraceparentVersion,
		Flags:    h.Flags,
		TraceID:  h.TraceID,
		ParentID: parentID,
	}, nil
}

// String generates the W3C traceparent header value according to
// https://www.w3.org/TR/trace-context/#version-format
func (h W3CTraceparent) String() string {
	return fmt.Sprintf(
		"%02x-%032x-%016x-%02x",
		h.Version, h.TraceID, h.ParentID, h.Flags,
	)
}
