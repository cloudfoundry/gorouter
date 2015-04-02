package db

import (
	"encoding/json"
	"fmt"

	"github.com/cloudfoundry/gunk/workpool"
	"github.com/cloudfoundry/storeadapter"
	"github.com/cloudfoundry/storeadapter/etcdstoreadapter"
)

//go:generate counterfeiter -o fakes/fake_db.go . DB
type DB interface {
	ReadRoutes() ([]Route, error)
	SaveRoute(route Route) error
	DeleteRoute(route Route) error
	Connect() error
	Disconnect() error
	WatchRouteChanges() (<-chan storeadapter.WatchEvent, chan<- bool, <-chan error)
}

type Route struct {
	Route   string `json:"route"`
	Port    int    `json:"port"`
	IP      string `json:"ip"`
	TTL     int    `json:"ttl"`
	LogGuid string `json:"log_guid"`
}

type etcd struct {
	storeAdapter *etcdstoreadapter.ETCDStoreAdapter
}

func NewETCD(nodeURLs []string) etcd {
	workpool := workpool.NewWorkPool(1)
	storeAdapter := etcdstoreadapter.NewETCDStoreAdapter(nodeURLs, workpool)
	return etcd{
		storeAdapter: storeAdapter,
	}
}

func (e etcd) Connect() error {
	return e.storeAdapter.Connect()
}

func (e etcd) Disconnect() error {
	return e.storeAdapter.Disconnect()
}

func (e etcd) ReadRoutes() ([]Route, error) {
	routes, err := e.storeAdapter.ListRecursively("/routes")
	if err != nil {
		return []Route{}, nil
	}
	var route Route
	listRoutes := []Route{}
	for _, node := range routes.ChildNodes {
		json.Unmarshal([]byte(node.Value), &route)
		listRoutes = append(listRoutes, route)
	}
	return listRoutes, nil
}

func (e etcd) SaveRoute(route Route) error {
	key := generateKey(route)
	routeJSON, _ := json.Marshal(route)
	node := storeadapter.StoreNode{
		Key:   key,
		Value: routeJSON,
		TTL:   uint64(route.TTL),
	}

	return e.storeAdapter.SetMulti([]storeadapter.StoreNode{node})
}

func (e etcd) DeleteRoute(route Route) error {
	key := generateKey(route)
	return e.storeAdapter.Delete(key)
}

func (e etcd) WatchRouteChanges() (<-chan storeadapter.WatchEvent, chan<- bool, <-chan error) {
	return e.storeAdapter.Watch("/routes")
}

func generateKey(route Route) string {
	return fmt.Sprintf("/routes/%s,%s:%d", route.Route, route.IP, route.Port)
}
