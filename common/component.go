package common

import (
	"encoding/json"
	"fmt"
	nats "github.com/cloudfoundry/gonats"
	"time"
)

type VcapComponent struct {
	Type        string
	Index       uint
	UUID        string
	Host        string
	Credentials []string
	Start       time.Time
	Uptime      time.Duration
}

var Component VcapComponent

func Register(c *VcapComponent, natsClient *nats.Client) {
	Component = *c
	if Component.Type == "" {
		panic("type is required")
	}

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

	Component.Start = time.Now()

	// TODO start /varz /healthz server here
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

func startStatusServer() {
}
