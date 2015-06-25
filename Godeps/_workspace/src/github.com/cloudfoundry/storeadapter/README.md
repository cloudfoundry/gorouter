storeadapter
============

Golang interface for ETCD/ZooKeeper style datastores

### `storeadapter`

The `storeadapter` is an generalized client for connecting to a Zookeeper/ETCD-like high availability store.  Writes are performed concurrently for optimal performance.


#### `fakestoreadapter`

Provides a fake in-memory implementation of the `storeadapter` to allow for unit tests that do not need to spin up a database.

#### `storerunner`

Brings up and manages the lifecycle of a live ETCD/ZooKeeper server cluster.
