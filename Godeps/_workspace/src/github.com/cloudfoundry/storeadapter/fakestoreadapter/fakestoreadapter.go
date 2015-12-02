package fakestoreadapter

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cloudfoundry/storeadapter"
)

type containerNode struct {
	dir   bool
	nodes map[string]*containerNode

	storeNode storeadapter.StoreNode
}

type FakeStoreAdapterErrorInjector struct {
	KeyRegexp *regexp.Regexp
	Error     error
}

func NewFakeStoreAdapterErrorInjector(keyRegexp string, err error) *FakeStoreAdapterErrorInjector {
	return &FakeStoreAdapterErrorInjector{
		KeyRegexp: regexp.MustCompile(keyRegexp),
		Error:     err,
	}
}

type FakeStoreAdapter struct {
	DidConnect    bool
	DidDisconnect bool

	ConnectErr        error
	DisconnectErr     error
	SetErrInjector    *FakeStoreAdapterErrorInjector
	GetErrInjector    *FakeStoreAdapterErrorInjector
	ListErrInjector   *FakeStoreAdapterErrorInjector
	DeleteErrInjector *FakeStoreAdapterErrorInjector
	CreateErrInjector *FakeStoreAdapterErrorInjector

	WatchErrChannel chan error

	rootNode *containerNode

	maintainedNodeName   string
	MaintainedNodeValue  []byte
	MaintainNodeError    error
	MaintainNodeStatus   chan bool
	releaseNodeChannel   chan chan bool
	OnReleaseNodeChannel func(chan chan bool)

	eventChannel chan storeadapter.WatchEvent
	sendEvents   bool
	sync.Mutex
}

func New() *FakeStoreAdapter {
	adapter := &FakeStoreAdapter{}
	adapter.Reset()
	return adapter
}

func (adapter *FakeStoreAdapter) Reset() {
	adapter.DidConnect = false
	adapter.DidDisconnect = false

	adapter.ConnectErr = nil
	adapter.DisconnectErr = nil
	adapter.SetErrInjector = nil
	adapter.GetErrInjector = nil
	adapter.ListErrInjector = nil
	adapter.DeleteErrInjector = nil
	adapter.CreateErrInjector = nil
	adapter.MaintainNodeStatus = make(chan bool, 1)

	adapter.rootNode = &containerNode{
		dir:   true,
		nodes: make(map[string]*containerNode),
	}

	adapter.sendEvents = false
	adapter.eventChannel = make(chan storeadapter.WatchEvent)
}

func (adapter *FakeStoreAdapter) GetMaintainedNodeName() string {
	adapter.Lock()
	defer adapter.Unlock()
	return adapter.maintainedNodeName
}

func (adapter *FakeStoreAdapter) Connect() error {
	adapter.DidConnect = true
	return adapter.ConnectErr
}

func (adapter *FakeStoreAdapter) Disconnect() error {
	adapter.Lock()
	defer adapter.Unlock()

	if !adapter.DidDisconnect {
		close(adapter.eventChannel)
		if adapter.WatchErrChannel != nil {
			close(adapter.WatchErrChannel)
		}
	}

	adapter.DidDisconnect = true
	return adapter.DisconnectErr
}

func (adapter *FakeStoreAdapter) sendEvent(prevNode *storeadapter.StoreNode, node *storeadapter.StoreNode, eventType storeadapter.EventType) {
	if adapter.sendEvents {
		go func() {
			adapter.Lock()
			defer adapter.Unlock()
			adapter.eventChannel <- storeadapter.WatchEvent{
				Type:     eventType,
				Node:     node,
				PrevNode: prevNode,
			}
		}()
	}
}

func (adapter *FakeStoreAdapter) SetMulti(nodes []storeadapter.StoreNode) error {
	adapter.Lock()
	defer adapter.Unlock()

	return adapter.setMulti(nodes)
}

func (adapter *FakeStoreAdapter) setMulti(nodes []storeadapter.StoreNode) error {
	var eventType storeadapter.EventType

	for _, node := range nodes {
		prevNode, err := adapter.get(node.Key)
		if err == nil {
			eventType = storeadapter.UpdateEvent
		} else {
			eventType = storeadapter.CreateEvent
		}

		if adapter.SetErrInjector != nil && adapter.SetErrInjector.KeyRegexp.MatchString(node.Key) {
			return adapter.SetErrInjector.Error
		}
		components := adapter.keyComponents(node.Key)

		container := adapter.rootNode
		for i, component := range components {
			existingNode, exists := container.nodes[component]
			if i == len(components)-1 {
				if exists && existingNode.dir {
					return storeadapter.ErrorNodeIsDirectory
				}
				container.nodes[component] = &containerNode{storeNode: node}
			} else {
				if exists {
					if !existingNode.dir {
						return storeadapter.ErrorNodeIsNotDirectory
					}
					container = existingNode
				} else {
					newContainer := &containerNode{dir: true, nodes: make(map[string]*containerNode)}
					container.nodes[component] = newContainer
					container = newContainer
				}
			}
		}

		adapter.sendEvent(&prevNode, &node, eventType)
	}

	return nil
}

func (adapter *FakeStoreAdapter) Create(node storeadapter.StoreNode) error {
	adapter.Lock()
	defer adapter.Unlock()

	if adapter.CreateErrInjector != nil && adapter.CreateErrInjector.KeyRegexp.MatchString(node.Key) {
		return adapter.CreateErrInjector.Error
	}

	_, err := adapter.get(node.Key)
	if err == nil {
		return storeadapter.ErrorKeyExists
	}

	return adapter.setMulti([]storeadapter.StoreNode{node})
}

func (adapter *FakeStoreAdapter) Get(key string) (storeadapter.StoreNode, error) {
	adapter.Lock()
	defer adapter.Unlock()

	return adapter.get(key)
}

func (adapter *FakeStoreAdapter) get(key string) (storeadapter.StoreNode, error) {
	if adapter.GetErrInjector != nil && adapter.GetErrInjector.KeyRegexp.MatchString(key) {
		return storeadapter.StoreNode{}, adapter.GetErrInjector.Error
	}

	container, err := adapter.walkToNode(key)
	if err != nil {
		return storeadapter.StoreNode{}, err
	}

	if container.dir {
		return storeadapter.StoreNode{}, storeadapter.ErrorNodeIsDirectory
	} else {
		return container.storeNode, nil
	}
}

func (adapter *FakeStoreAdapter) walkToNode(key string) (*containerNode, error) {
	container := adapter.rootNode
	for _, component := range adapter.keyComponents(key) {
		var exists bool
		container, exists = container.nodes[component]
		if !exists {
			return nil, storeadapter.ErrorKeyNotFound
		}
	}

	return container, nil
}

func (adapter *FakeStoreAdapter) ListRecursively(key string) (storeadapter.StoreNode, error) {
	adapter.Lock()
	defer adapter.Unlock()

	if adapter.ListErrInjector != nil && adapter.ListErrInjector.KeyRegexp.MatchString(key) {
		return storeadapter.StoreNode{}, adapter.ListErrInjector.Error
	}

	container, err := adapter.walkToNode(key)
	if err != nil {
		return storeadapter.StoreNode{}, err
	}

	if !container.dir {
		return storeadapter.StoreNode{}, storeadapter.ErrorNodeIsNotDirectory
	}

	return adapter.listContainerNode(key, container), nil
}

func (adapter *FakeStoreAdapter) listContainerNode(key string, container *containerNode) storeadapter.StoreNode {
	childNodes := []storeadapter.StoreNode{}

	for nodeKey, node := range container.nodes {
		if node.dir {
			if key == "/" {
				nodeKey = "/" + nodeKey
			} else {
				nodeKey = key + "/" + nodeKey
			}
			childNodes = append(childNodes, adapter.listContainerNode(nodeKey, node))
		} else {
			childNodes = append(childNodes, node.storeNode)
		}
	}

	return storeadapter.StoreNode{
		Key:        key,
		Dir:        true,
		ChildNodes: childNodes,
	}
}

func (adapter *FakeStoreAdapter) Delete(keys ...string) error {
	adapter.Lock()
	defer adapter.Unlock()

	return adapter.deleteKeys(keys...)
}

func (adapter *FakeStoreAdapter) deleteKeys(keys ...string) error {
	for _, key := range keys {
		node, _ := adapter.get(key)

		if adapter.DeleteErrInjector != nil && adapter.DeleteErrInjector.KeyRegexp.MatchString(key) {
			return adapter.DeleteErrInjector.Error
		}

		components := adapter.keyComponents(key)
		container := adapter.rootNode
		parentNode := adapter.rootNode
		for _, component := range components {
			var exists bool
			parentNode = container
			container, exists = container.nodes[component]
			if !exists {
				return storeadapter.ErrorKeyNotFound
			}
		}

		leaf := parentNode.nodes[components[len(components)-1]]
		if leaf.dir {
			var keysToDelete []string
			for key, _ := range leaf.nodes {
				childKey := strings.Join(append(components, key), "/")
				keysToDelete = append(keysToDelete, childKey)
			}
			adapter.deleteKeys(keysToDelete...)
		}

		delete(parentNode.nodes, components[len(components)-1])
		adapter.sendEvent(&node, nil, storeadapter.DeleteEvent)
	}

	return nil
}

func (adapter *FakeStoreAdapter) DeleteLeaves(keys ...string) error {
	panic("not implemented")
}

func (adapter *FakeStoreAdapter) CompareAndDelete(nodes ...storeadapter.StoreNode) error {
	adapter.Lock()
	defer adapter.Unlock()

	if len(nodes) != 1 {
		panic("not implemented for zero/multiple nodes")
	}

	node := nodes[0]

	existingNode, err := adapter.get(node.Key)

	if err != nil {
		return err
	}

	if string(node.Value) != string(existingNode.Value) {
		return storeadapter.ErrorKeyComparisonFailed
	}

	return adapter.deleteKeys(node.Key)
}

func (adapter *FakeStoreAdapter) CompareAndDeleteByIndex(node ...storeadapter.StoreNode) error {
	panic("not implemented")
}

func (adapter *FakeStoreAdapter) UpdateDirTTL(key string, ttl uint64) error {
	container, err := adapter.walkToNode(key)
	if err != nil {
		return err
	}
	if !container.dir {
		return storeadapter.ErrorNodeIsNotDirectory
	}

	go func() {
		time.Sleep(time.Duration(ttl) * time.Second)
		adapter.Delete(key)
	}()
	return nil
}

func (adapter *FakeStoreAdapter) Update(node storeadapter.StoreNode) error {
	panic("not implemented")
}

func (adapter *FakeStoreAdapter) CompareAndSwap(oldNode storeadapter.StoreNode, newNode storeadapter.StoreNode) error {
	adapter.Lock()
	defer adapter.Unlock()

	existingNode, err := adapter.get(newNode.Key)

	if err != nil {
		return err
	}

	if string(oldNode.Value) != string(existingNode.Value) {
		return storeadapter.ErrorKeyComparisonFailed
	}

	return adapter.setMulti([]storeadapter.StoreNode{newNode})
}

func (adapter *FakeStoreAdapter) CompareAndSwapByIndex(oldNodeIndex uint64, newNode storeadapter.StoreNode) error {
	panic("not implemented")
}

func (adapter *FakeStoreAdapter) Watch(key string) (events <-chan storeadapter.WatchEvent, stop chan<- bool, errors <-chan error) {
	adapter.Lock()
	defer adapter.Unlock()
	adapter.sendEvents = true
	adapter.WatchErrChannel = make(chan error, 1)

	// We haven't implemented stop yet

	return adapter.eventChannel, nil, adapter.WatchErrChannel
}

func (adapter *FakeStoreAdapter) keyComponents(key string) (components []string) {
	for _, s := range strings.Split(key, "/") {
		if s != "" {
			components = append(components, s)
		}
	}

	return components
}

func (adapter *FakeStoreAdapter) MaintainNode(storeNode storeadapter.StoreNode) (status <-chan bool, releaseNode chan chan bool, err error) {
	adapter.Lock()
	defer adapter.Unlock()

	adapter.maintainedNodeName = storeNode.Key
	adapter.MaintainedNodeValue = storeNode.Value
	adapter.releaseNodeChannel = make(chan chan bool, 1)
	if adapter.OnReleaseNodeChannel != nil {
		go adapter.OnReleaseNodeChannel(adapter.releaseNodeChannel)
	}

	return adapter.MaintainNodeStatus, adapter.releaseNodeChannel, adapter.MaintainNodeError
}
