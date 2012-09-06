package common

import (
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	"runtime"
	"sync"
	"time"
)

type VcapComponent struct {
	// These fields are from individual components
	Type        string      `json:"type"`
	Index       uint        `json:"index"`
	Host        string      `json:"host"`
	Credentials []string    `json:"credentials"`
	Healthz     interface{} `json:"-"`
	Varz        interface{} `json:"-"`

	// These fields are automatically generated
	UUID   string        `json:"uuid"`
	Start  time.Time     `json:"start"`
	Uptime time.Duration `json:"uptime"`
}

type Healthz struct {
	Health interface{} `json:"health"`
}

type Varz struct {
	sync.Mutex

	Uptime   time.Duration    `json:"uptime"`
	Start    time.Time        `json:"start"`
	MemStats runtime.MemStats `json:"memstats"`
	Var      interface{}      `json:"var"`
}

var healthz Healthz
var varz Varz
var Component VcapComponent

func UpdateHealthz() *Healthz {
	return &healthz
}

func UpdateVarz() *Varz {
	varz.Lock()
	defer varz.Unlock()

	varz.Uptime = time.Since(varz.Start)
	runtime.ReadMemStats(&varz.MemStats)

	return &varz
}

func Register(c *VcapComponent, natsClient *nats.Client) {
	Component = *c
	if Component.Type == "" {
		panic("type is required")
	}

	Component.Start = time.Now()
	Component.UUID = fmt.Sprintf("%d-%s", Component.Index, GenerateUUID())

	if Component.Host == "" {
		host, err := LocalIP()
		if err != nil {
			panic(err)
		}

		port, err := GrabEphemeralPort()
		if err != nil {
			panic(err)
		}

		Component.Host = fmt.Sprintf("%s:%s", host, port)
	}

	if Component.Credentials == nil || len(Component.Credentials) != 2 {
		user := GenerateUUID()
		password := GenerateUUID()

		Component.Credentials = []string{user, password}
	}

	// Init healthz/varz
	healthz.Health = Component.Healthz
	varz.Start = Component.Start
	varz.Var = Component.Varz

	go startStatusServer()

	// subscribe nats
	discover := natsClient.NewSubscription("vcap.component.discover")
	discover.Subscribe()

	go func() {
		for m := range discover.Inbox {
			updateUptime()

			bytes, _ := json.Marshal(Component)
			natsClient.Publish(string(m.ReplyTo), bytes)
		}
	}()

	bytes, _ := json.Marshal(Component)
	natsClient.Publish("vcap.component.announce", bytes)
}

func updateUptime() {
	Component.Uptime = time.Since(Component.Start)
}
