package handlers

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// W3CTracestateEntry represents a Tracestate entry: a key value pair
type W3CTracestateEntry struct {
	Key string
	Val string
}

func (s W3CTracestateEntry) String() string {
	return fmt.Sprintf("%s=%s", s.Key, s.Val)
}

// W3CTracestate is an alias for a slice W3CTracestateEntry; has helper funcs
type W3CTracestate []W3CTracestateEntry

func (s W3CTracestate) String() string {
	states := make([]string, 0)

	for i := 1; i <= len(s); i++ {
		states = append(states, s[len(s)-i].String())
	}

	return strings.Join(states, ",")
}

func NextW3CTracestate(tenantID string, parentID []byte) W3CTracestateEntry {
	var key string

	if tenantID == "" {
		key = W3CVendorID
	} else {
		key = fmt.Sprintf("%s@%s", tenantID, W3CVendorID)
	}

	return W3CTracestateEntry{Key: key, Val: hex.EncodeToString(parentID)}
}

func (s W3CTracestate) Next(tenantID string, parentID []byte) W3CTracestate {
	entry := NextW3CTracestate(tenantID, parentID)

	newEntries := make(W3CTracestate, 0)

	// We should not persist entries which have the same key
	for _, existingEntry := range s {
		if existingEntry.Key != entry.Key {
			newEntries = append(newEntries, existingEntry)
		}
	}

	return append(newEntries, entry)
}

func ParseW3CTracestate(header string) W3CTracestate {
	parsed := make(W3CTracestate, 0)

	// Arbitrarily ignore large traces for performance reasons
	if len(header) > 2048 {
		return parsed
	}

	states := strings.Split(header, ",")

	// We loop in reverse because the headers are oldest at the end
	for i := 1; i <= len(states); i++ {
		pair := states[len(states)-i]
		split := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(split) == 2 {
			parsed = append(parsed, W3CTracestateEntry{Key: split[0], Val: split[1]})
		}
	}

	return parsed
}

// NewW3CTracestate generates a new set of W3C tracestate pairs according to
// https://www.w3.org/TR/trace-context/#version-format
// Initially it is populated with the current tracestate determined by
// arguments tenantID and parentID
func NewW3CTracestate(tenantID string, parentID []byte) W3CTracestate {
	return W3CTracestate{NextW3CTracestate(tenantID, parentID)}
}
