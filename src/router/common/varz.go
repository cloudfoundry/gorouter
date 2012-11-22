package common

import (
	"encoding/json"
	"sync"
)

type varz_ struct {
	// Static common metrics
	NumCores int `json:"num_cores"`

	// Dynamic common metrics
	MemStat int64   `json:"mem"`
	Cpu     float64 `json:"cpu"`

	Uptime Duration `json:"uptime"`
}

type Varz struct {
	sync.Mutex

	varz_

	// Every component's unique metrics
	UniqueVarz interface{}
}

func transform(x interface{}, y *map[string]interface{}) error {
	var b []byte
	var err error

	b, err = json.Marshal(x)
	if err != nil {
		return err
	}

	err = json.Unmarshal(b, y)
	if err != nil {
		return err
	}

	return nil
}

func (v *Varz) MarshalJSON() ([]byte, error) {
	r := make(map[string]interface{})

	var err error

	err = transform(v.UniqueVarz, &r)
	if err != nil {
		return nil, err
	}

	err = transform(v.varz_, &r)
	if err != nil {
		return nil, err
	}

	err = transform(Component, &r)
	if err != nil {
		return nil, err
	}

	return json.Marshal(r)
}
