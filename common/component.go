package common

import (
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	"log"
	"runtime"
	"syscall"
	"time"
)

var Component VcapComponent
var healthz *Healthz
var varz *Varz

type VcapComponent struct {
	// These fields are from individual components
	Type        string      `json:"type"`
	Index       uint        `json:"index"`
	Host        string      `json:"host"`
	Credentials []string    `json:"credentials"`
	Config      interface{} `json:"-"`
	Varz        *Varz       `json:"-"`
	Healthz     interface{} `json:"-"`

	// These fields are automatically generated
	UUID   string    `json:"uuid"`
	Start  time.Time `json:"start"`
	Uptime Duration  `json:"uptime"`
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

	r := new(syscall.Rusage)
	syscall.Getrusage(syscall.RUSAGE_SELF, r)

	varz.MemStat = r.Maxrss
	varz.Cpu = r.Utime.Nano() + r.Stime.Nano()
	varz.Uptime = time.Since(Component.Start)

	return varz
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

	varz = Component.Varz
	varz.NumCores = runtime.NumCPU()
	// The component doesn't provide a way to encode the unique metrics, use the default one
	if varz.EncodeUniqueVarz == nil {
		varz.EncodeUniqueVarz = DefaultUniqueVarzEncoder
	}

	healthz = &Healthz{Component.Healthz}

	go startStatusServer()

	// subscribe nats
	discover := natsClient.NewSubscription("vcap.component.discover")
	discover.Subscribe()

	go func() {
		for m := range discover.Inbox {
			Component.Uptime = Duration(time.Since(Component.Start))
			bytes, _ := json.Marshal(Component)
			natsClient.Publish(string(m.ReplyTo), bytes)
		}
	}()

	bytes, err := json.Marshal(Component)
	if err != nil {
		log.Println(err)
	}
	natsClient.Publish("vcap.component.announce", bytes)
}
