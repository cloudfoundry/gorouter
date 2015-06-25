package route

import (
	"encoding/json"
	"fmt"
	"time"
)

func NewEndpoint(appId, host string, port uint16, privateInstanceId string,
	tags map[string]string, staleThresholdInSeconds int, routeServiceUrl string) *Endpoint {
	return &Endpoint{
		ApplicationId:     appId,
		addr:              fmt.Sprintf("%s:%d", host, port),
		Tags:              tags,
		PrivateInstanceId: privateInstanceId,
		staleThreshold:    time.Duration(staleThresholdInSeconds) * time.Second,
		RouteServiceUrl:   routeServiceUrl,
	}
}

type Endpoint struct {
	ApplicationId     string
	addr              string
	Tags              map[string]string
	PrivateInstanceId string
	staleThreshold    time.Duration
	RouteServiceUrl   string
}

func (e *Endpoint) MarshalJSON() ([]byte, error) {
	var jsonObj struct {
		Address         string `json:"address"`
		TTL             int    `json:"ttl"`
		RouteServiceUrl string `json:"route_service_url,omitempty"`
	}

	jsonObj.Address = e.addr
	jsonObj.RouteServiceUrl = e.RouteServiceUrl
	jsonObj.TTL = int(e.staleThreshold.Seconds())
	return json.Marshal(jsonObj)
}

func (e *Endpoint) CanonicalAddr() string {
	return e.addr
}

func (e *Endpoint) ToLogData() interface{} {
	return struct {
		ApplicationId   string
		Addr            string
		Tags            map[string]string
		RouteServiceUrl string
	}{
		e.ApplicationId,
		e.addr,
		e.Tags,
		e.RouteServiceUrl,
	}
}
