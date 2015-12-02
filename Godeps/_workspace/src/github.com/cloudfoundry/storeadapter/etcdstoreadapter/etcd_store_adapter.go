package etcdstoreadapter

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/cloudfoundry/gunk/workpool"
	"github.com/cloudfoundry/storeadapter"
	"github.com/coreos/go-etcd/etcd"
	"github.com/nu7hatch/gouuid"
)

type ETCDStoreAdapter struct {
	client            *etcd.Client
	workPool          *workpool.WorkPool
	inflightWatches   map[chan bool]bool
	inflightWatchLock *sync.Mutex
}

func New(options *ETCDOptions, workPool *workpool.WorkPool) (*ETCDStoreAdapter, error) {
	if options.IsSSL {
		return newTLSClient(options.ClusterUrls, options.CertFile, options.KeyFile, options.CAFile, workPool)
	}

	return newHTTPClient(options.ClusterUrls, workPool), nil
}

func newHTTPClient(urls []string, workPool *workpool.WorkPool) *ETCDStoreAdapter {
	client := etcd.NewClient(urls)
	return newAdapter(client, workPool)
}

func newTLSClient(urls []string, cert, key, caCert string, workPool *workpool.WorkPool) (*ETCDStoreAdapter, error) {
	client, err := NewETCDTLSClient(urls, cert, key, caCert)
	if err != nil {
		return nil, err
	}

	return newAdapter(client, workPool), nil
}

func NewETCDTLSClient(urls []string, certFile, keyFile, caCertFile string) (*etcd.Client, error) {
	client := etcd.NewClient(urls)
	tlsCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{tlsCert},
		InsecureSkipVerify: false,
	}

	tr := &http.Transport{
		TLSClientConfig: tlsConfig,
		Dial: (&net.Dialer{
			Timeout:   time.Second,
			KeepAlive: time.Second,
		}).Dial,
	}
	client.SetTransport(tr)
	if caCertFile != "" {
		err = client.AddRootCA(caCertFile)
	}
	if err != nil {
		return nil, err
	}

	return client, nil
}

func newAdapter(client *etcd.Client, workPool *workpool.WorkPool) *ETCDStoreAdapter {
	client.SetConsistency(etcd.STRONG_CONSISTENCY)

	return &ETCDStoreAdapter{
		client:            client,
		workPool:          workPool,
		inflightWatches:   map[chan bool]bool{},
		inflightWatchLock: &sync.Mutex{},
	}
}

func (adapter *ETCDStoreAdapter) Connect() error {
	if !adapter.client.SyncCluster() {
		return errors.New("sync cluster failed")
	}

	return nil
}

func (adapter *ETCDStoreAdapter) Disconnect() error {
	adapter.workPool.Stop()
	adapter.cancelInflightWatches()

	return nil
}

func (adapter *ETCDStoreAdapter) isEventIndexClearedError(err error) bool {
	return adapter.etcdErrorCode(err) == 401
}

func (adapter *ETCDStoreAdapter) etcdErrorCode(err error) int {
	if err != nil {
		switch err.(type) {
		case etcd.EtcdError:
			return err.(etcd.EtcdError).ErrorCode
		case *etcd.EtcdError:
			return err.(*etcd.EtcdError).ErrorCode
		}
	}
	return 0
}

func (adapter *ETCDStoreAdapter) convertError(err error) error {
	switch adapter.etcdErrorCode(err) {
	case 501:
		return storeadapter.ErrorTimeout
	case 100:
		return storeadapter.ErrorKeyNotFound
	case 102:
		return storeadapter.ErrorNodeIsDirectory
	case 105:
		return storeadapter.ErrorKeyExists
	case 101:
		return storeadapter.ErrorKeyComparisonFailed
	}

	return err
}

func (adapter *ETCDStoreAdapter) SetMulti(nodes []storeadapter.StoreNode) error {
	results := make(chan error, len(nodes))

	for _, node := range nodes {
		node := node
		adapter.workPool.Submit(func() {
			_, err := adapter.client.Set(node.Key, string(node.Value), node.TTL)
			results <- err
		})
	}

	var err error
	numReceived := 0
	for numReceived < len(nodes) {
		result := <-results
		numReceived++
		if err == nil {
			err = result
		}
	}

	return adapter.convertError(err)
}

func (adapter *ETCDStoreAdapter) Get(key string) (storeadapter.StoreNode, error) {
	done := make(chan bool, 1)
	var response *etcd.Response
	var err error

	//we route through the worker pool to enable usage tracking
	adapter.workPool.Submit(func() {
		response, err = adapter.client.Get(key, false, false)
		done <- true
	})

	<-done

	if err != nil {
		return storeadapter.StoreNode{}, adapter.convertError(err)
	}

	if response.Node.Dir {
		return storeadapter.StoreNode{}, storeadapter.ErrorNodeIsDirectory
	}

	return storeadapter.StoreNode{
		Key:   response.Node.Key,
		Value: []byte(response.Node.Value),
		Dir:   response.Node.Dir,
		TTL:   uint64(response.Node.TTL),
		Index: response.Node.ModifiedIndex,
	}, nil
}

func (adapter *ETCDStoreAdapter) ListRecursively(key string) (storeadapter.StoreNode, error) {
	done := make(chan bool, 1)
	var response *etcd.Response
	var err error

	//we route through the worker pool to enable usage tracking
	adapter.workPool.Submit(func() {
		response, err = adapter.client.Get(key, false, true)
		done <- true
	})

	<-done

	if err != nil {
		return storeadapter.StoreNode{}, adapter.convertError(err)
	}

	if !response.Node.Dir {
		return storeadapter.StoreNode{}, storeadapter.ErrorNodeIsNotDirectory
	}

	if len(response.Node.Nodes) == 0 {
		return storeadapter.StoreNode{Key: key, Dir: true, Value: []byte{}, ChildNodes: []storeadapter.StoreNode{}, Index: response.Node.ModifiedIndex}, nil
	}

	return *adapter.makeStoreNode(response.Node), nil
}

func (adapter *ETCDStoreAdapter) Create(node storeadapter.StoreNode) error {
	results := make(chan error, 1)

	adapter.workPool.Submit(func() {
		_, err := adapter.client.Create(node.Key, string(node.Value), node.TTL)
		results <- err
	})

	return adapter.convertError(<-results)
}

func (adapter *ETCDStoreAdapter) Update(node storeadapter.StoreNode) error {
	results := make(chan error, 1)

	adapter.workPool.Submit(func() {
		_, err := adapter.client.Update(node.Key, string(node.Value), node.TTL)
		results <- err
	})

	return adapter.convertError(<-results)
}

func (adapter *ETCDStoreAdapter) CompareAndSwap(oldNode storeadapter.StoreNode, newNode storeadapter.StoreNode) error {
	results := make(chan error, 1)

	adapter.workPool.Submit(func() {
		_, err := adapter.client.CompareAndSwap(
			newNode.Key,
			string(newNode.Value),
			newNode.TTL,
			string(oldNode.Value),
			0,
		)

		results <- err
	})

	return adapter.convertError(<-results)
}

func (adapter *ETCDStoreAdapter) CompareAndSwapByIndex(oldNodeIndex uint64, newNode storeadapter.StoreNode) error {
	results := make(chan error, 1)

	adapter.workPool.Submit(func() {
		_, err := adapter.client.CompareAndSwap(
			newNode.Key,
			string(newNode.Value),
			newNode.TTL,
			"",
			oldNodeIndex,
		)

		results <- err
	})

	return adapter.convertError(<-results)
}

func (adapter *ETCDStoreAdapter) Delete(keys ...string) error {
	results := make(chan error, len(keys))

	for _, key := range keys {
		key := key
		adapter.workPool.Submit(func() {
			_, err := adapter.client.Delete(key, true)
			results <- err
		})
	}

	var err error
	numReceived := 0
	for numReceived < len(keys) {
		result := <-results
		numReceived++
		if err == nil {
			err = result
		}
	}

	return adapter.convertError(err)
}

func (adapter *ETCDStoreAdapter) DeleteLeaves(keys ...string) error {
	results := make(chan error, len(keys))

	for _, key := range keys {
		key := key
		adapter.workPool.Submit(func() {
			_, err := adapter.client.DeleteDir(key)
			results <- err
		})
	}

	var err error
	numReceived := 0
	for numReceived < len(keys) {
		result := <-results
		numReceived++
		if err == nil {
			err = result
		}
	}

	return adapter.convertError(err)
}

func (adapter *ETCDStoreAdapter) CompareAndDelete(nodes ...storeadapter.StoreNode) error {
	results := make(chan error, len(nodes))

	for _, node := range nodes {
		node := node
		adapter.workPool.Submit(func() {
			_, err := adapter.client.CompareAndDelete(
				node.Key,
				string(node.Value),
				0,
			)
			results <- err
		})
	}

	var err error
	numReceived := 0
	for numReceived < len(nodes) {
		result := <-results
		numReceived++
		if err == nil {
			err = result
		}
	}

	return adapter.convertError(err)
}

func (adapter *ETCDStoreAdapter) CompareAndDeleteByIndex(nodes ...storeadapter.StoreNode) error {
	results := make(chan error, len(nodes))

	for _, node := range nodes {
		node := node
		adapter.workPool.Submit(func() {
			_, err := adapter.client.CompareAndDelete(
				node.Key,
				"",
				node.Index,
			)
			results <- err
		})
	}

	var err error
	numReceived := 0
	for numReceived < len(nodes) {
		result := <-results
		numReceived++
		if err == nil {
			err = result
		}
	}

	return adapter.convertError(err)
}

func (adapter *ETCDStoreAdapter) UpdateDirTTL(key string, ttl uint64) error {
	response, err := adapter.Get(key)
	if err == nil && response.Dir == false {
		return storeadapter.ErrorNodeIsNotDirectory
	}

	results := make(chan error, 1)

	adapter.workPool.Submit(func() {
		_, err = adapter.client.UpdateDir(key, ttl)
		results <- err
	})

	return adapter.convertError(<-results)
}

func (adapter *ETCDStoreAdapter) Watch(key string) (<-chan storeadapter.WatchEvent, chan<- bool, <-chan error) {
	events := make(chan storeadapter.WatchEvent)
	errors := make(chan error)
	stop := make(chan bool, 1)

	go adapter.dispatchWatchEvents(key, events, stop, errors)

	time.Sleep(100 * time.Millisecond) //give the watcher a chance to connect

	return events, stop, errors
}

func (adapter *ETCDStoreAdapter) dispatchWatchEvents(key string, events chan<- storeadapter.WatchEvent, stop chan bool, errors chan<- error) {
	var index uint64
	adapter.registerInflightWatch(stop)

	defer close(events)
	defer close(errors)
	defer adapter.unregisterInflightWatch(stop)

	for {
		response, err := adapter.client.Watch(key, index, true, nil, stop)
		if err != nil {
			if adapter.isEventIndexClearedError(err) {
				index = 0
				continue
			} else if err == etcd.ErrWatchStoppedByUser {
				return
			} else {
				errors <- adapter.convertError(err)
				return
			}
		}

		event, err := adapter.makeWatchEvent(response)
		if err != nil {
			errors <- err
			return
		} else {
			events <- event
		}

		index = response.Node.ModifiedIndex + 1
	}
}

func (adapter *ETCDStoreAdapter) registerInflightWatch(stop chan bool) {
	adapter.inflightWatchLock.Lock()
	defer adapter.inflightWatchLock.Unlock()
	adapter.inflightWatches[stop] = true
}

func (adapter *ETCDStoreAdapter) unregisterInflightWatch(stop chan bool) {
	adapter.inflightWatchLock.Lock()
	defer adapter.inflightWatchLock.Unlock()
	delete(adapter.inflightWatches, stop)
}

func (adapter *ETCDStoreAdapter) cancelInflightWatches() {
	adapter.inflightWatchLock.Lock()
	defer adapter.inflightWatchLock.Unlock()

	for stop := range adapter.inflightWatches {
		select {
		case _, ok := <-stop:
			if ok {
				close(stop)
			}
		default:
			close(stop)
		}
	}
}

func (adapter *ETCDStoreAdapter) makeStoreNode(etcdNode *etcd.Node) *storeadapter.StoreNode {
	if etcdNode == nil {
		return nil
	}

	if etcdNode.Dir {
		node := storeadapter.StoreNode{
			Key:        etcdNode.Key,
			Dir:        true,
			Value:      []byte{},
			ChildNodes: []storeadapter.StoreNode{},
			TTL:        uint64(etcdNode.TTL),
			Index:      uint64(etcdNode.ModifiedIndex),
		}

		for _, child := range etcdNode.Nodes {
			node.ChildNodes = append(node.ChildNodes, *adapter.makeStoreNode(child))
		}

		return &node
	} else {
		return &storeadapter.StoreNode{
			Key:   etcdNode.Key,
			Value: []byte(etcdNode.Value),
			TTL:   uint64(etcdNode.TTL),
			Index: uint64(etcdNode.ModifiedIndex),
		}
	}
}

func (adapter *ETCDStoreAdapter) makeWatchEvent(event *etcd.Response) (storeadapter.WatchEvent, error) {
	var eventType storeadapter.EventType

	node := event.Node
	switch event.Action {
	case "delete", "compareAndDelete":
		eventType = storeadapter.DeleteEvent
		node = nil
	case "create":
		eventType = storeadapter.CreateEvent
	case "set", "update", "compareAndSwap":
		eventType = storeadapter.UpdateEvent
	case "expire":
		eventType = storeadapter.ExpireEvent
		node = nil
	default:
		return storeadapter.WatchEvent{}, fmt.Errorf("unknown event: %s", event.Action)
	}

	return storeadapter.WatchEvent{
		Type:     eventType,
		Node:     adapter.makeStoreNode(node),
		PrevNode: adapter.makeStoreNode(event.PrevNode),
	}, nil
}

func (adapter *ETCDStoreAdapter) MaintainNode(storeNode storeadapter.StoreNode) (<-chan bool, chan (chan bool), error) {
	if storeNode.TTL == 0 {
		return nil, nil, storeadapter.ErrorInvalidTTL
	}

	if len(storeNode.Value) == 0 {
		guid, err := uuid.NewV4()
		if err != nil {
			return nil, nil, err
		}

		storeNode.Value = []byte(guid.String())
	}

	releaseNode := make(chan chan bool)
	nodeStatus := make(chan bool)

	go adapter.maintainNode(storeNode, nodeStatus, releaseNode)

	return nodeStatus, releaseNode, nil
}

func (adapter *ETCDStoreAdapter) maintainNode(storeNode storeadapter.StoreNode, nodeStatus chan bool, releaseNode chan (chan bool)) {
	frequency := 2
	maintenanceInterval := time.Duration(storeNode.TTL) * time.Second / time.Duration(frequency)
	timer := time.NewTimer(0)

	created := false
	owned := false
	frequencyCycle := 0

	for {
		retryInterval := 2 * time.Second
		select {
		case <-timer.C:
			for {
				if created {
					_, err := adapter.client.CompareAndSwap(
						storeNode.Key,
						string(storeNode.Value),
						storeNode.TTL,
						string(storeNode.Value),
						0,
					)

					if err == nil {
						frequencyCycle++
						owned = true
						elapsed := time.Duration(0)
						if frequencyCycle == frequency {
							elapsed = elapsedChannelSend(nodeStatus, true)
							frequencyCycle = 0
						}
						timer.Reset(maintenanceInterval - elapsed)
						break
					}

					frequencyCycle = 0

					if owned {
						owned = false
						nodeStatus <- false
					}

					err = adapter.convertError(err)
					if err == storeadapter.ErrorKeyNotFound {
						created = false
						continue
					}

					retryInterval = retryBackOff(retryInterval, maintenanceInterval)
					timer.Reset(retryInterval)
					break
				} else {
					frequencyCycle = 0

					_, err := adapter.client.Create(storeNode.Key, string(storeNode.Value), storeNode.TTL)
					if err == nil {
						created = true
						owned = true

						elapsed := elapsedChannelSend(nodeStatus, true)
						timer.Reset(maintenanceInterval - elapsed)

						break
					}

					err = adapter.convertError(err)
					if err == storeadapter.ErrorKeyExists {
						created = true
						continue
					}

					retryInterval = retryBackOff(retryInterval, maintenanceInterval)
					timer.Reset(retryInterval)
					break
				}
			}

		case released := <-releaseNode:
			adapter.client.CompareAndDelete(storeNode.Key, string(storeNode.Value), 0)
			timer.Stop()
			close(nodeStatus)
			if released != nil {
				close(released)
			}
			return
		}
	}
}

func elapsedChannelSend(channel chan bool, val bool) time.Duration {
	start := time.Now()
	channel <- val
	return time.Now().Sub(start)
}

func retryBackOff(current, max time.Duration) time.Duration {
	current = current * 2
	if current > max {
		current = max
	}
	return current
}
