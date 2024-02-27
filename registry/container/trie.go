package container

import (
	"strings"

	"code.cloudfoundry.org/gorouter/route"
)

// package name inspired by golang package that includes heap, list and ring.
type Trie struct {
	Segment    string
	Pool       *route.EndpointPool
	ChildNodes map[string]*Trie
	Parent     *Trie
}

// Find returns a *route.EndpointPool that matches exactly the URI parameter, nil if no match was found.
func (r *Trie) Find(uri route.Uri) *route.EndpointPool {
	key := strings.TrimPrefix(uri.String(), "/")
	node := r

	for {
		segmentValue, nextKey, found := strings.Cut(key, "/")

		matchingChild, ok := node.ChildNodes[segmentValue]
		if !ok {
			return nil
		}

		node = matchingChild

		if !found {
			break
		}

		key = nextKey
	}

	if nil != node.Pool {
		return node.Pool
	}

	return nil
}

// MatchUri returns the longest route that matches the URI parameter and has endpoints, nil if nothing matches.
func (r *Trie) MatchUri(uri route.Uri) *route.EndpointPool {
	key := strings.TrimPrefix(uri.String(), "/")
	node := r
	var lastPool *route.EndpointPool

	for {
		segmentValue, nextKey, found := strings.Cut(key, "/")

		matchingChild, ok := node.ChildNodes[segmentValue]
		if !ok {
			break
		}

		node = matchingChild

		// Matching pools with endpoints is what we want
		if nil != node.Pool && !node.Pool.IsEmpty() {
			lastPool = node.Pool
		}

		if !found {
			break
		}

		key = nextKey
	}

	// Prefer lastPool over node.Pool since we know it must have endpoints
	if nil != node.Pool && nil == lastPool {
		return node.Pool
	}

	return lastPool
}

func (r *Trie) Insert(uri route.Uri, value *route.EndpointPool) *Trie {
	key := strings.TrimPrefix(uri.String(), "/")
	node := r

	for {
		segmentValue, nextKey, found := strings.Cut(key, "/")

		matchingChild, ok := node.ChildNodes[segmentValue]

		if !ok {
			matchingChild = NewTrie()
			matchingChild.Segment = segmentValue
			matchingChild.Parent = node
			node.ChildNodes[segmentValue] = matchingChild
		}

		node = matchingChild

		if !found {
			break
		}

		key = nextKey
	}

	node.Pool = value
	return node
}

func (r *Trie) Delete(uri route.Uri) bool {
	key := strings.TrimPrefix(uri.String(), "/")
	node := r
	initialKey := key

	for {
		segmentValue, nextKey, found := strings.Cut(key, "/")

		// It is currently impossible to Delete a non-existent path. This invariant is
		// provided by the fact that a call to Find is done before Delete in the registry.
		matchingChild := node.ChildNodes[segmentValue]

		node = matchingChild

		if !found {
			break
		}

		key = nextKey
	}
	node.Pool = nil
	r.deleteEmptyNodes(initialKey)

	return true
}

func (r *Trie) deleteEmptyNodes(key string) {
	node := r
	nodeToKeep := r
	var nodeToRemove *Trie

	for {
		segmentValue, nextKey, found := strings.Cut(key, "/")

		matchingChild := node.ChildNodes[segmentValue]

		if nil == nodeToRemove && nil == matchingChild.Pool && len(matchingChild.ChildNodes) < 2 {
			nodeToRemove = matchingChild
		} else if nil != matchingChild.Pool || len(matchingChild.ChildNodes) > 1 {
			nodeToKeep = matchingChild
			nodeToRemove = nil
		}

		node = matchingChild

		if !found {
			break
		}

		key = nextKey
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

// Snip removes an empty EndpointPool from a node and trims empty leaf nodes from the Trie
func (r *Trie) Snip() {
	if r.Pool != nil && r.Pool.IsEmpty() {
		r.Pool = nil
	}
	if (r.Pool != nil && !r.Pool.IsEmpty()) || r.isRoot() || !r.isLeaf() {
		return
	}
	delete(r.Parent.ChildNodes, r.Segment)
	r.Parent.Snip()
}

func (r *Trie) ToPath() string {
	if r.Parent.isRoot() {
		return r.Segment
	}
	return r.Parent.ToPath() + "/" + r.Segment
}

func (r *Trie) ToMap() map[route.Uri]*route.EndpointPool {
	return r.toMap(r.Segment, make(map[route.Uri]*route.EndpointPool))
}

func (r *Trie) toMap(segment string, m map[route.Uri]*route.EndpointPool) map[route.Uri]*route.EndpointPool {
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
