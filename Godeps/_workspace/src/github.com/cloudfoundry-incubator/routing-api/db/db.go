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
	Route           string `json:"route"`
	Port            uint16 `json:"port"`
	IP              string `json:"ip"`
	TTL             int    `json:"ttl"`
	LogGuid         string `json:"log_guid"`
	RouteServiceUrl string `json:"route_service_url,omitempty"`
}

type etcd struct {
	storeAdapter *etcdstoreadapter.ETCDStoreAdapter
}

func NewETCD(nodeURLs []string, maxWorkers uint) (*etcd, error) {
	workpool, err := workpool.NewWorkPool(int(maxWorkers))
	if err != nil {
		return nil, err
	}

	storeAdapter := etcdstoreadapter.NewETCDStoreAdapter(nodeURLs, workpool)
	return &etcd{
		storeAdapter: storeAdapter,
	}, nil
}

func (e *etcd) Connect() error {
	return e.storeAdapter.Connect()
}

func (e *etcd) Disconnect() error {
	return e.storeAdapter.Disconnect()
}

func (e *etcd) ReadRoutes() ([]Route, error) {
	routes, err := e.storeAdapter.ListRecursively("/routes")
	if err != nil {
		return []Route{}, nil
	}

	listRoutes := []Route{}
	for _, node := range routes.ChildNodes {
		route := Route{}
		json.Unmarshal([]byte(node.Value), &route)
		listRoutes = append(listRoutes, route)
	}
	return listRoutes, nil
}

func (e *etcd) SaveRoute(route Route) error {
	key := generateKey(route)
	routeJSON, _ := json.Marshal(route)
	node := storeadapter.StoreNode{
		Key:   key,
		Value: routeJSON,
		TTL:   uint64(route.TTL),
	}

	return e.storeAdapter.SetMulti([]storeadapter.StoreNode{node})
}

func (e *etcd) DeleteRoute(route Route) error {
	key := generateKey(route)
	err := e.storeAdapter.Delete(key)
	if err != nil && err.Error() == "the requested key could not be found" {
		err = DBError{Type: KeyNotFound, Message: "The specified route could not be found."}
	}
	return err
}

func (e *etcd) WatchRouteChanges() (<-chan storeadapter.WatchEvent, chan<- bool, <-chan error) {
	return e.storeAdapter.Watch("/routes")
}

func generateKey(route Route) string {
	return fmt.Sprintf("/routes/%s,%s:%d", route.Route, route.IP, route.Port)
}
