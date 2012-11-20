package common

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
)

type Varz struct {
	sync.Mutex `json:"-"`

	// Static common metrics
	NumCores int `json:"num_cores"`

	// Dynamic common metrics
	MemStat int64   `json:"mem"`
	Cpu     float64 `json:"cpu"`

	Uptime Duration `json:"uptime"`

	// Every component's unique metrics
	UniqueVarz interface{} `encode:"yes"`
}

func (v *Varz) MarshalJSON() ([]byte, error) {
	d := parseVarz(v)

	// Merge component's information from VcapComponent
	c := parseVarz(Component)
	for k, v := range c {
		if d[k] == nil { // prevent fields of varz being overridden
			d[k] = v
		}
	}

	return json.Marshal(d)
}

func parseVarz(varz interface{}) map[string]interface{} {
	data := make(map[string]interface{})

	parseVarzRecursively(varz, data)

	return data
}

func parseVarzRecursively(varz interface{}, data map[string]interface{}) {
	typeInfo := reflect.TypeOf(varz)
	var value reflect.Value
	if typeInfo.Kind() == reflect.Ptr {
		typeInfo = typeInfo.Elem()
		value = reflect.ValueOf(varz).Elem()
	} else {
		value = reflect.ValueOf(varz)
	}

	if typeInfo.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < typeInfo.NumField(); i++ {
		t := typeInfo.Field(i)

		if !isExported(t.Name) {
			continue
		}

		k := t.Tag.Get("json")
		if k == "-" {
			continue
		}

		if t.Tag.Get("encode") == "yes" {
			parseVarzRecursively(value.Field(i).Interface(), data)
		} else {
			if k == "" {
				k = strings.ToLower(t.Name)
			}
			data[k] = value.Field(i).Interface()
		}
	}
}

func isExported(name string) bool {
	if len(name) < 1 {
		return false
	}

	if name[0] >= 'A' && name[0] <= 'Z' {
		return true
	}

	return false
}
