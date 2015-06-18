package router

import (
	"strings"

	"github.com/cloudfoundry/gorouter/route"
)

type RegistryMessage struct {
	Host                    string            `json:"host"`
	Port                    uint16            `json:"port"`
	Uris                    []route.Uri       `json:"uris"`
	Tags                    map[string]string `json:"tags"`
	App                     string            `json:"app"`
	StaleThresholdInSeconds int               `json:"stale_threshold_in_seconds"`
	RouteServiceUrl         string            `json:"route_service_url"`
	PrivateInstanceId       string            `json:"private_instance_id"`
}

func (rm *RegistryMessage) makeEndpoint() *route.Endpoint {
	return route.NewEndpoint(rm.App, rm.Host, rm.Port, rm.PrivateInstanceId, rm.Tags, rm.StaleThresholdInSeconds, rm.RouteServiceUrl)
}

func (rm *RegistryMessage) ValidateMessage() bool {
	return rm.RouteServiceUrl == "" || strings.HasPrefix(rm.RouteServiceUrl, "https")
}
