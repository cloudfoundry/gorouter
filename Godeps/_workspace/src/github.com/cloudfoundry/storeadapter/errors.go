package storeadapter

import (
	"errors"
)

var (
	ErrorKeyNotFound         = errors.New("the requested key could not be found")
	ErrorNodeIsDirectory     = errors.New("node is a directory, not a leaf")
	ErrorNodeIsNotDirectory  = errors.New("node is a leaf, not a directory")
	ErrorTimeout             = errors.New("store request timed out")
	ErrorInvalidFormat       = errors.New("node has invalid format")
	ErrorInvalidTTL          = errors.New("got an invalid TTL")
	ErrorKeyExists           = errors.New("a node already exists at the requested key")
	ErrorKeyComparisonFailed = errors.New("node comparison failed")
)
