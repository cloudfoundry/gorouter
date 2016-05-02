package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/cloudfoundry-incubator/routing-api/models"
	"github.com/coreos/etcd/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/coreos/etcd/client"
)

//go:generate counterfeiter -o fakes/fake_watcher.go ../../../coreos/etcd/client/keys.go Watcher
//go:generate counterfeiter -o fakes/fake_client.go ../../../coreos/etcd/client/client.go Client
//go:generate counterfeiter -o fakes/fake_keys_api.go ../../../coreos/etcd/client/keys.go KeysAPI
//go:generate counterfeiter -o fakes/fake_db.go . DB
type DB interface {
	ReadRoutes() ([]models.Route, error)
	SaveRoute(route models.Route) error
	DeleteRoute(route models.Route) error

	ReadTcpRouteMappings() ([]models.TcpRouteMapping, error)
	SaveTcpRouteMapping(tcpMapping models.TcpRouteMapping) error
	DeleteTcpRouteMapping(tcpMapping models.TcpRouteMapping) error

	ReadRouterGroups() (models.RouterGroups, error)
	SaveRouterGroup(routerGroup models.RouterGroup) error

	Connect() error

	CancelWatches()
	WatchRouteChanges(filter string) (<-chan Event, <-chan error, context.CancelFunc)
}

const (
	TCP_MAPPING_BASE_KEY  string = "/v1/tcp_routes/router_groups"
	HTTP_ROUTE_BASE_KEY   string = "/routes"
	ROUTER_GROUP_BASE_KEY string = "/v1/router_groups"

	maxRetries = 3
)

var ErrorConflict = errors.New("etcd failed to compare")

type etcd struct {
	client     client.Client
	keysAPI    client.KeysAPI
	ctx        context.Context
	cancelFunc context.CancelFunc
}

func NewETCD(nodeURLs []string) (DB, error) {
	cfg := client.Config{
		Endpoints: nodeURLs,
		Transport: client.DefaultTransport,
	}

	c, err := client.New(cfg)
	if err != nil {
		return nil, err
	}
	keysAPI := client.NewKeysAPI(c)

	ctx, cancel := context.WithCancel(context.Background())

	return New(c, keysAPI, ctx, cancel), nil
}

func New(client client.Client,
	keys client.KeysAPI,
	ctx context.Context,
	cancelFunc context.CancelFunc) DB {
	return &etcd{
		client:     client,
		keysAPI:    keys,
		ctx:        ctx,
		cancelFunc: cancelFunc,
	}
}

func (e *etcd) Connect() error {
	return e.client.Sync(e.ctx)
}

func (e *etcd) CancelWatches() {
	e.cancelFunc()
}

func (e *etcd) ReadRoutes() ([]models.Route, error) {
	getOpts := &client.GetOptions{
		Recursive: true,
	}
	response, err := e.keysAPI.Get(context.Background(), HTTP_ROUTE_BASE_KEY, getOpts)
	if err != nil {
		return []models.Route{}, nil
	}

	listRoutes := []models.Route{}
	for _, node := range response.Node.Nodes {
		route := models.Route{}
		json.Unmarshal([]byte(node.Value), &route)
		listRoutes = append(listRoutes, route)
	}
	return listRoutes, nil
}

func readOpts() *client.GetOptions {
	return &client.GetOptions{
		Recursive: true,
	}
}

func createOpts(ttl int) *client.SetOptions {
	return &client.SetOptions{
		TTL:       time.Duration(ttl) * time.Second,
		PrevExist: "false",
	}
}

func updateOptsWithTTL(ttl int, prevIndex uint64) *client.SetOptions {
	return &client.SetOptions{
		TTL:       time.Duration(ttl) * time.Second,
		PrevIndex: prevIndex,
	}
}

func updateOpts(prevIndex uint64) *client.SetOptions {
	return &client.SetOptions{
		PrevIndex: prevIndex,
	}
}

func ctx() context.Context {
	return context.Background()
}

func (e *etcd) SaveRoute(route models.Route) error {
	key := generateHttpRouteKey(route)

	retries := 0

	for retries <= maxRetries {
		response, err := e.keysAPI.Get(context.Background(), key, readOpts())

		// Update
		if response != nil && err == nil {
			var existingRoute models.Route
			err = json.Unmarshal([]byte(response.Node.Value), &existingRoute)
			if err != nil {
				return err
			}

			route.ModificationTag = existingRoute.ModificationTag
			route.ModificationTag.Increment()

			routeJSON, _ := json.Marshal(route)
			_, err = e.keysAPI.Set(context.Background(), key, string(routeJSON), updateOptsWithTTL(route.TTL, response.Node.ModifiedIndex))
			if err == nil {
				break
			}
		} else if cerr, ok := err.(client.Error); ok && cerr.Code == client.ErrorCodeKeyNotFound { //create
			// Delete came in between a read and an update
			if retries > 0 {
				return ErrorConflict
			}

			var tag models.ModificationTag
			tag, err = models.NewModificationTag()
			if err != nil {
				return err
			}
			route.ModificationTag = tag
			routeJSON, _ := json.Marshal(route)
			_, err = e.keysAPI.Set(ctx(), key, string(routeJSON), createOpts(route.TTL))
			if err == nil {
				break
			}
		}

		// only retry on a compare and swap error
		if cerr, ok := err.(client.Error); ok && cerr.Code == client.ErrorCodeTestFailed {
			retries++
		} else {
			return err
		}
	}

	if retries > maxRetries {
		return ErrorConflict
	}
	return nil
}

func (e *etcd) DeleteRoute(route models.Route) error {
	key := generateHttpRouteKey(route)

	deleteOpt := &client.DeleteOptions{}
	_, err := e.keysAPI.Delete(context.Background(), key, deleteOpt)
	if err != nil {
		cerr, ok := err.(client.Error)
		if ok && cerr.Code == client.ErrorCodeKeyNotFound {
			err = DBError{Type: KeyNotFound, Message: "The specified route could not be found."}
		}
	}
	return err
}

func (e *etcd) WatchRouteChanges(filter string) (<-chan Event, <-chan error, context.CancelFunc) {
	events := make(chan Event)
	errors := make(chan error)

	cxt, cancel := context.WithCancel(e.ctx)

	go e.dispatchWatchEvents(cxt, filter, events, errors)

	time.Sleep(100 * time.Millisecond) //give the watcher a chance to connect

	return events, errors, cancel
}

func (e *etcd) dispatchWatchEvents(cxt context.Context, key string, events chan<- Event, errors chan<- error) {
	watchOpt := &client.WatcherOptions{Recursive: true}
	watcher := e.keysAPI.Watcher(key, watchOpt)

	defer close(events)
	defer close(errors)

	for {
		resp, err := watcher.Next(cxt)
		if err != nil {
			if err, ok := err.(client.Error); ok {
				if err.Code == client.ErrorCodeEventIndexCleared {
					watcher = e.keysAPI.Watcher(key, watchOpt)
					continue
				}
			}

			if err != context.Canceled {
				errors <- err
			}
			return
		}

		event, err := NewEvent(resp)
		if err != nil {
			errors <- err
			return
		} else {
			events <- event
		}
	}
}

func (e *etcd) SaveRouterGroup(routerGroup models.RouterGroup) error {
	if routerGroup.Guid == "" {
		return errors.New("Invalid router group: missing guid")
	}

	// fetch router groups
	routerGroups, err := e.ReadRouterGroups()
	if err != nil {
		return err
	}
	// check for uniqueness of router group name
	for _, rg := range routerGroups {
		if rg.Guid != routerGroup.Guid && rg.Name == routerGroup.Name {
			msg := fmt.Sprintf("The RouterGroup with name: %s already exists", routerGroup.Name)
			return DBError{Type: UniqueField, Message: msg}
		}
	}

	key := generateRouterGroupKey(routerGroup)
	getOpts := &client.GetOptions{
		Recursive: true,
	}
	rg, err := e.keysAPI.Get(context.Background(), key, getOpts)
	if err == nil {
		current := models.RouterGroup{}
		json.Unmarshal([]byte(rg.Node.Value), &current)
		if routerGroup.Name != current.Name {
			return DBError{Type: NonUpdatableField, Message: "The RouterGroup Name cannot be updated"}
		}
	}
	json, _ := json.Marshal(routerGroup)
	setOpt := &client.SetOptions{}
	_, err = e.keysAPI.Set(context.Background(), key, string(json), setOpt)

	return err
}

func (e *etcd) ReadRouterGroups() (models.RouterGroups, error) {
	getOpts := &client.GetOptions{
		Recursive: true,
	}
	response, err := e.keysAPI.Get(context.Background(), ROUTER_GROUP_BASE_KEY, getOpts)
	if err != nil {
		return models.RouterGroups{}, nil
	}

	results := []models.RouterGroup{}
	for _, node := range response.Node.Nodes {
		routerGroup := models.RouterGroup{}
		json.Unmarshal([]byte(node.Value), &routerGroup)
		results = append(results, routerGroup)
	}
	return results, nil
}

func generateHttpRouteKey(route models.Route) string {
	return fmt.Sprintf("%s/%s,%s:%d", HTTP_ROUTE_BASE_KEY, url.QueryEscape(route.Route), route.IP, route.Port)
}

func generateRouterGroupKey(routerGroup models.RouterGroup) string {
	return fmt.Sprintf("%s/%s", ROUTER_GROUP_BASE_KEY, routerGroup.Guid)
}

func (e *etcd) ReadTcpRouteMappings() ([]models.TcpRouteMapping, error) {
	getOpts := &client.GetOptions{
		Recursive: true,
	}
	tcpMappings, err := e.keysAPI.Get(context.Background(), TCP_MAPPING_BASE_KEY, getOpts)
	if err != nil {
		return []models.TcpRouteMapping{}, nil
	}

	listMappings := []models.TcpRouteMapping{}
	for _, routerGroupNode := range tcpMappings.Node.Nodes {
		for _, externalPortNode := range routerGroupNode.Nodes {
			for _, mappingNode := range externalPortNode.Nodes {
				tcpMapping := models.TcpRouteMapping{}
				json.Unmarshal([]byte(mappingNode.Value), &tcpMapping)
				listMappings = append(listMappings, tcpMapping)
			}
		}
	}
	return listMappings, nil
}

func (e *etcd) SaveTcpRouteMapping(tcpMapping models.TcpRouteMapping) error {
	key := generateTcpRouteMappingKey(tcpMapping)

	retries := 0
	for retries <= maxRetries {
		response, err := e.keysAPI.Get(context.Background(), key, readOpts())

		// Update
		if response != nil && err == nil {
			var existingTcpRouteMapping models.TcpRouteMapping

			err = json.Unmarshal([]byte(response.Node.Value), &existingTcpRouteMapping)
			if err != nil {
				return err
			}

			tcpMapping.ModificationTag = existingTcpRouteMapping.ModificationTag
			tcpMapping.ModificationTag.Increment()

			tcpRouteJSON, _ := json.Marshal(tcpMapping)
			_, err = e.keysAPI.Set(ctx(), key, string(tcpRouteJSON), updateOptsWithTTL(int(tcpMapping.TTL), response.Node.ModifiedIndex))
		} else if cerr, ok := err.(client.Error); ok && cerr.Code == client.ErrorCodeKeyNotFound { //create
			// Delete came in between a read and update
			if retries > 0 {
				return ErrorConflict
			}

			var tag models.ModificationTag
			tag, err = models.NewModificationTag()
			if err != nil {
				return err
			}

			tcpMapping.ModificationTag = tag
			tcpRouteMappingJSON, _ := json.Marshal(tcpMapping)
			_, err = e.keysAPI.Set(ctx(), key, string(tcpRouteMappingJSON), createOpts(int(tcpMapping.TTL)))
		}

		// return when create or update is successful
		if err == nil {
			return nil
		}

		// only retry on a compare and swap error
		if cerr, ok := err.(client.Error); ok && cerr.Code == client.ErrorCodeTestFailed {
			retries++
		} else {
			return err
		}
	}

	// number of retries exceeded
	return ErrorConflict
}

func (e *etcd) DeleteTcpRouteMapping(tcpMapping models.TcpRouteMapping) error {
	key := generateTcpRouteMappingKey(tcpMapping)
	deleteOpt := &client.DeleteOptions{}
	_, err := e.keysAPI.Delete(context.Background(), key, deleteOpt)

	if err != nil {
		cerr, ok := err.(client.Error)
		if ok && cerr.Code == client.ErrorCodeKeyNotFound {
			err = DBError{Type: KeyNotFound, Message: "The specified route (" + tcpMapping.String() + ") could not be found."}
		}
	}

	return err
}

func generateTcpRouteMappingKey(tcpMapping models.TcpRouteMapping) string {
	// Generating keys following this pattern
	// /v1/tcp_routes/router_groups/{router_guid}/{port}/{host-ip}:{host-port}
	return fmt.Sprintf("%s/%s/%d/%s:%d", TCP_MAPPING_BASE_KEY,
		tcpMapping.TcpRoute.RouterGroupGuid, tcpMapping.TcpRoute.ExternalPort, tcpMapping.HostIP, tcpMapping.HostPort)
}
