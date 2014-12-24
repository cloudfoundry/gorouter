package route

import (
	"encoding/json"
	"fmt"
	"time"
)

func NewEndpoint(appId, host string, port uint16, privateInstanceId string,
	tags map[string]string, staleThresholdInSeconds int) *Endpoint {
	return &Endpoint{
		ApplicationId:     appId,
		addr:              fmt.Sprintf("%s:%d", host, port),
		Tags:              tags,
		PrivateInstanceId: privateInstanceId,
		staleThreshold:    time.Duration(staleThresholdInSeconds) * time.Second,
	}
}

type Endpoint struct {
	ApplicationId     string
	addr              string
	Tags              map[string]string
	PrivateInstanceId string
	staleThreshold    time.Duration
}

func (e *Endpoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.addr)
}

func (e *Endpoint) CanonicalAddr() string {
	return e.addr
}

func (e *Endpoint) ToLogData() interface{} {
	return struct {
		ApplicationId string
		Addr          string
		Tags          map[string]string
	}{
		e.ApplicationId,
		e.addr,
		e.Tags,
	}
}
