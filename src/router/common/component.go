package common

import (
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	steno "github.com/cloudfoundry/gosteno"
	"runtime"
	"time"
)

var Component VcapComponent
var healthz *Healthz
var varz *Varz

var procStat *ProcessStatus

type VcapComponent struct {
	// These fields are from individual components
	Type        string       `json:"type"`
	Index       uint         `json:"index"`
	Host        string       `json:"host"`
	Credentials []string     `json:"credentials"`
	Config      interface{}  `json:"config"`
	Varz        *Varz        `json:"-"`
	Healthz     interface{}  `json:"-"`
	Logger      steno.Logger `json:"-"`

	// These fields are automatically generated
	UUID   string   `json:"uuid"`
	Start  Time     `json:"start"`
	Uptime Duration `json:"uptime"`
}

type Healthz struct {
	Health interface{} `json:"health"`
}

func UpdateHealthz() *Healthz {
	return healthz
}

func UpdateVarz() *Varz {
	varz.Lock()
	defer varz.Unlock()

	varz.MemStat = procStat.MemRss
	varz.Cpu = procStat.CpuUsage
	varz.Uptime = Component.Start.Elapsed()

	return varz
}

func Register(c *VcapComponent, natsClient *nats.Client) {
	Component = *c
	if Component.Type == "" {
		log.Fatal("Component type is required")
		panic("type is required")
	}

	Component.Start = Time(time.Now())
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

	if Component.Logger != nil {
		log = Component.Logger
	}

	varz = Component.Varz
	varz.NumCores = runtime.NumCPU()

	procStat = NewProcessStatus()

	healthz = &Healthz{Component.Healthz}

	go startStatusServer()

	// subscribe nats
	discover := natsClient.NewSubscription("vcap.component.discover")
	discover.Subscribe()

	go func() {
		for m := range discover.Inbox {
			Component.Uptime = Component.Start.Elapsed()
			bytes, _ := json.Marshal(Component)
			natsClient.Publish(string(m.ReplyTo), bytes)
		}
	}()

	bytes, err := json.Marshal(Component)
	if err != nil {
		log.Error(err.Error())
	}
	natsClient.Publish("vcap.component.announce", bytes)
	log.Debugf("Component %s registered successfully", Component.Type)
}
