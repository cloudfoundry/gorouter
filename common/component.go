package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"time"

	"github.com/apcera/nats"
	. "github.com/cloudfoundry/gorouter/common/http"
	"github.com/cloudfoundry/yagnats"
	"github.com/pivotal-golang/lager"
	"github.com/pivotal-golang/localip"
)

var procStat *ProcessStatus

type VcapComponent struct {
	Config     interface{}               `json:"-"`
	Varz       *Varz                     `json:"-"`
	Healthz    *Healthz                  `json:"-"`
	InfoRoutes map[string]json.Marshaler `json:"-"`
	Logger     lager.Logger              `json:"-"`

	listener net.Listener
	statusCh chan error
	quitCh   chan struct{}
}

type RouterStart struct {
	Id                               string   `json:"id"`
	Hosts                            []string `json:"hosts"`
	MinimumRegisterIntervalInSeconds int      `json:"minimumRegisterIntervalInSeconds"`
	PruneThresholdInSeconds          int      `json:"pruneThresholdInSeconds"`
}

func (c *VcapComponent) UpdateVarz() {
	c.Varz.Lock()
	defer c.Varz.Unlock()

	procStat.RLock()
	c.Varz.MemStat = procStat.MemRss
	c.Varz.Cpu = procStat.CpuUsage
	procStat.RUnlock()
	c.Varz.Uptime = c.Varz.StartTime.Elapsed()
}

func (c *VcapComponent) Start() error {
	if c.Varz.Type == "" {
		err := errors.New("type is required")
		log.Error("Component type is required", err)
		return err
	}

	c.quitCh = make(chan struct{}, 1)
	c.Varz.StartTime = Time(time.Now())
	uuid, err := GenerateUUID()
	if err != nil {
		return err
	}
	c.Varz.UUID = fmt.Sprintf("%d-%s", c.Varz.Index, uuid)

	if c.Varz.Host == "" {
		host, err := localip.LocalIP()
		if err != nil {
			log.Error("error-getting-localIP", err)
			return err
		}

		port, err := localip.LocalPort()
		if err != nil {
			log.Error("error-getting-localPort", err)
			return err
		}

		c.Varz.Host = fmt.Sprintf("%s:%d", host, port)
	}

	if c.Varz.Credentials == nil || len(c.Varz.Credentials) != 2 {
		user, err := GenerateUUID()
		if err != nil {
			return err
		}
		password, err := GenerateUUID()
		if err != nil {
			return err
		}

		c.Varz.Credentials = []string{user, password}
	}

	if c.Logger != nil {
		log = c.Logger
	}

	c.Varz.NumCores = runtime.NumCPU()

	procStat = NewProcessStatus()

	c.ListenAndServe()
	return nil
}

func (c *VcapComponent) Register(mbusClient yagnats.NATSConn) error {
	mbusClient.Subscribe("vcap.component.discover", func(msg *nats.Msg) {
		if msg.Reply == "" {
			log.Info(fmt.Sprintf("Received message with empty reply on subject %s", msg.Subject))
			return
		}

		c.Varz.Uptime = c.Varz.StartTime.Elapsed()
		b, e := json.Marshal(c.Varz)
		if e != nil {
			log.Error("error-json-marshaling", e)
			return
		}

		mbusClient.Publish(msg.Reply, b)
	})

	b, e := json.Marshal(c.Varz)
	if e != nil {
		log.Error("error-json-marshaling", e)
		return e
	}

	mbusClient.Publish("vcap.component.announce", b)

	log.Info(fmt.Sprintf("Component %s registered successfully", c.Varz.Type))
	return nil
}

func (c *VcapComponent) Stop() {
	close(c.quitCh)
	if c.listener != nil {
		c.listener.Close()
		<-c.statusCh
	}
}

func (c *VcapComponent) ListenAndServe() {
	hs := http.NewServeMux()

	hs.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Connection", "close")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, c.Healthz.Value())
	})

	hs.HandleFunc("/varz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Connection", "close")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		enc := json.NewEncoder(w)
		c.UpdateVarz()
		enc.Encode(c.Varz)
	})

	for path, marshaler := range c.InfoRoutes {
		m := marshaler
		hs.HandleFunc(path, func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Connection", "close")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			enc := json.NewEncoder(w)
			enc.Encode(m)
		})
	}

	f := func(user, password string) bool {
		return user == c.Varz.Credentials[0] && password == c.Varz.Credentials[1]
	}

	s := &http.Server{
		Addr:         c.Varz.Host,
		Handler:      &BasicAuth{hs, f},
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	c.statusCh = make(chan error, 1)
	l, err := net.Listen("tcp", c.Varz.Host)
	if err != nil {
		c.statusCh <- err
		return
	}
	c.listener = l

	go func() {
		err = s.Serve(l)
		select {
		case <-c.quitCh:
			c.statusCh <- nil

		default:
			c.statusCh <- err
		}
	}()
}
