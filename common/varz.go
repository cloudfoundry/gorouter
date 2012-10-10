package common

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"
)

type UniqueVarzEncoder func(m interface{}) map[string]interface{}

type Varz struct {
	sync.Mutex

	// Static common metrics
	NumCores int

	// Dynamic common metrics
	MemStat int64
	Cpu     int64
	Uptime  time.Duration

	// Every component's unique metrics
	UniqueVarz       interface{}       `json:"-"`
	EncodeUniqueVarz UniqueVarzEncoder `json:"-"`
}

func (v *Varz) MarshalJSON() ([]byte, error) {
	s := v.encodeComponentInfo()
	c := v.encodeCommonVarz()
	u := v.EncodeUniqueVarz(v.UniqueVarz)

	for k, v := range c {
		s[k] = v
	}
	for k, v := range u {
		s[k] = v
	}

	return json.Marshal(s)
}

func (v *Varz) encodeComponentInfo() map[string]interface{} {
	s := make(map[string]interface{})

	s["type"] = Component.Type
	s["index"] = Component.Index
	s["host"] = Component.Host
	s["uuid"] = Component.UUID
	s["start"] = formatTime(Component.Start)
	s["config"] = Component.Config

	return s
}

func (v *Varz) encodeCommonVarz() map[string]interface{} {
	c := make(map[string]interface{})

	c["num_cores"] = v.NumCores

	c["mem"] = v.MemStat
	// TODO: A simple but not ideal way to show the cpu usage, should be improved
	c["cpu"] = float64(v.Cpu) / float64(v.Uptime.Nanoseconds())
	c["uptime"] = formatDuration(v.Uptime)

	return c
}

func formatDuration(d time.Duration) string {
	t := int64(d.Seconds())
	day := t / (60 * 60 * 24)
	t = t % (60 * 60 * 24)
	hour := t / (60 * 60)
	t = t % (60 * 60)
	min := t / 60
	sec := t % 60

	ds := fmt.Sprintf("%dd:%dh:%dm:%ds", day, hour, min, sec)
	return ds
}

func formatTime(t time.Time) string {
	f := "2006-01-02 15:04:05 -0700"
	return t.Format(f)
}

func DefaultUniqueVarzEncoder(m interface{}) map[string]interface{} {
	d := make(map[string]interface{})
	parseFromUniqueVarz(m, d)
	return d
}

func parseFromUniqueVarz(m interface{}, data map[string]interface{}) {
	typeInfo := reflect.TypeOf(m)
	var value reflect.Value
	if typeInfo.Kind() == reflect.Ptr {
		typeInfo = typeInfo.Elem()
		value = reflect.ValueOf(m).Elem()
	} else {
		value = reflect.ValueOf(m)
	}

	if typeInfo.Kind() != reflect.Struct {
		log.Printf("%v type can't have attributes inspected\n", typeInfo.Kind())
		return
	}

	for i := 0; i < typeInfo.NumField(); i++ {
		t := typeInfo.Field(i)
		if !isExported(t.Name) {
			continue
		}
		if !t.Anonymous {
			k := t.Tag.Get("json")
			if k == "-" {
				continue
			}
			if k == "" {
				k = strings.ToLower(t.Name)
			}
			data[k] = value.Field(i).Interface()
		} else {
			// TODO: the level of recursion should be concerned in case of nested anonymous fields
			parseFromUniqueVarz(value.Field(i).Interface(), data)
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
