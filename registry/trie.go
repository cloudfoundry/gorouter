package registry

import (
	"strings"

	"github.com/cloudfoundry/gorouter/route"
)

type Trie struct {
	Segment    string
	Pool       *route.Pool
	ChildNodes map[string]*Trie
	Parent     *Trie
}

func (r *Trie) Find(key string) (*route.Pool, bool) {
	node := r
	var lastPool *route.Pool

	for _, SegmentValue := range parts(key) {
		matchingChild, ok := node.ChildNodes[SegmentValue]

		if !ok {
			break
		}

		node = matchingChild

		if nil != node.Pool {
			lastPool = node.Pool
		}
	}

	if nil != node.Pool {
		return node.Pool, true
	}

	if nil != lastPool {
		return lastPool, true
	}

	return nil, false
}

func (r *Trie) Insert(key string, value *route.Pool) *Trie {
	node := r

	for _, SegmentValue := range parts(key) {
		matchingChild, ok := node.ChildNodes[SegmentValue]

		if !ok {
			matchingChild = NewTrie()
			matchingChild.Segment = SegmentValue
			matchingChild.Parent = node
			node.ChildNodes[SegmentValue] = matchingChild
		}

		node = matchingChild
	}

	node.Pool = value
	return node
}

func (r *Trie) Delete(key string) bool {
	node := r

	for _, SegmentValue := range parts(key) {
		matchingChild, _ := node.ChildNodes[SegmentValue]

		node = matchingChild
	}
	node.Pool = nil
	r.deleteEmptyNodes(key)

	return true
}

func (r *Trie) deleteEmptyNodes(key string) {
	node := r
	nodeToKeep := r
	var nodeToRemove *Trie

	for _, SegmentValue := range parts(key) {
		matchingChild, _ := node.ChildNodes[SegmentValue]

		if nil == nodeToRemove && nil == matchingChild.Pool && len(matchingChild.ChildNodes) < 2 {
			nodeToRemove = matchingChild
		} else if nil != matchingChild.Pool || len(matchingChild.ChildNodes) > 1 {
			nodeToKeep = matchingChild
			nodeToRemove = nil
		}

		node = matchingChild
	}

	if node.isLeaf() {
		nodeToRemove.Parent = nil
		delete(nodeToKeep.ChildNodes, nodeToRemove.Segment)
	}
}

func (r *Trie) PoolCount() int {
	result := 0

	f := func(_ *Trie) {
		result += 1
	}

	r.EachNodeWithPool(f)

	return result
}

func (r *Trie) EachNodeWithPool(f func(*Trie)) {
	if r.Pool != nil {
		f(r)
	}

	for _, child := range r.ChildNodes {
		child.EachNodeWithPool(f)
	}
}

func (r *Trie) EndpointCount() int {
	m := make(map[string]struct{})

	return len(r.endpointCount(m))
}

func (r *Trie) endpointCount(m map[string]struct{}) map[string]struct{} {

	if r.Pool != nil {
		f := func(e *route.Endpoint) {
			m[e.CanonicalAddr()] = struct{}{}
		}
		r.Pool.Each(f)
	}

	for _, child := range r.ChildNodes {
		child.endpointCount(m)
	}

	return m
}

func (r *Trie) PruneDeadLeaves() {
	if r.isLeaf() {
		r.Snip()
	}
	for _, child := range r.ChildNodes {
		child.PruneDeadLeaves()
	}
}

func NewTrie() *Trie {
	return &Trie{ChildNodes: make(map[string]*Trie), Segment: ""}
}

func (r *Trie) Snip() {
	if (r.Pool != nil && !r.Pool.IsEmpty()) || r.isRoot() || !r.isLeaf() {
		return
	}
	delete(r.Parent.ChildNodes, r.Segment)
	r.Parent.Snip()
}

func (r *Trie) ToMap() map[route.Uri]*route.Pool {
	return r.toMap(r.Segment, make(map[route.Uri]*route.Pool))
}

func (r *Trie) toMap(segment string, m map[route.Uri]*route.Pool) map[route.Uri]*route.Pool {
	if r.Pool != nil {
		m[route.Uri(segment)] = r.Pool
	}

	for _, child := range r.ChildNodes {
		var newseg string
		if len(segment) == 0 {
			newseg = segment + child.Segment
		} else {
			newseg = segment + "/" + child.Segment
		}
		child.toMap(newseg, m)
	}

	return m
}

func (r *Trie) isRoot() bool {
	return r.Parent == nil
}

func (r *Trie) isLeaf() bool {
	return len(r.ChildNodes) == 0
}

func parts(key string) []string {
	return strings.Split(strings.TrimPrefix(key, "/"), "/")
}
