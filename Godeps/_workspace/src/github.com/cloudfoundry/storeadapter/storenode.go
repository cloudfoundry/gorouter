package storeadapter

import (
	"path"
	"strings"
)

type StoreNode struct {
	Key        string
	Value      []byte
	Dir        bool
	TTL        uint64
	ChildNodes []StoreNode
	Index      uint64
}

func (self StoreNode) Lookup(childKey string) (StoreNode, bool) {
	lookupKey := path.Join(self.Key, childKey)

	for _, node := range self.ChildNodes {
		if node.Key == lookupKey {
			return node, true
		}
	}

	return StoreNode{}, false
}

func (self StoreNode) KeyComponents() []string {
	return strings.Split(self.Key, "/")[1:]
}
