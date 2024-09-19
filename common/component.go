package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sync"
	"syscall"
	"time"

	"code.cloudfoundry.org/gorouter/common/health"
	. "code.cloudfoundry.org/gorouter/common/http"
	"code.cloudfoundry.org/gorouter/common/schema"
	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/logger"
	"code.cloudfoundry.org/gorouter/test_util"
	"code.cloudfoundry.org/localip"
	"github.com/nats-io/nats.go"
	"github.com/uber-go/zap"
)

const RefreshInterval time.Duration = time.Second * 1

var log logger.Logger

type ProcessStatus struct {
	sync.RWMutex
	rusage      *syscall.Rusage
	lastCpuTime int64
	stopSignal  chan bool
	stopped     bool

	CpuUsage float64
	MemRss   int64
}

func NewProcessStatus() *ProcessStatus {
	p := new(ProcessStatus)
	p.rusage = new(syscall.Rusage)

	go func() {
		timer := time.NewTicker(RefreshInterval)
		for {
			select {
			case <-timer.C:
				p.Update()
			case <-p.stopSignal:
				return
			}
		}
	}()

	return p
}

func (p *ProcessStatus) Update() {
	e := syscall.Getrusage(syscall.RUSAGE_SELF, p.rusage)
	if e != nil {
		log.Fatal("failed-to-get-rusage", zap.Error(e))
	}

	p.Lock()
	defer p.Unlock()
	p.MemRss = int64(p.rusage.Maxrss)

	t := p.rusage.Utime.Nano() + p.rusage.Stime.Nano()
	p.CpuUsage = float64(t-p.lastCpuTime) / float64(RefreshInterval.Nanoseconds())
	p.lastCpuTime = t
}

func (p *ProcessStatus) StopUpdate() {
	p.Lock()
	defer p.Unlock()
	if !p.stopped {
		p.stopped = true
		p.stopSignal <- true
		p.stopSignal = nil
	}
}

var procStat *ProcessStatus

type VcapComponent struct {
	Config     interface{}  `json:"-"`
	Varz       *health.Varz `json:"-"`
	Health     http.Handler
	InfoRoutes map[string]json.Marshaler `json:"-"`
	Logger     logger.Logger             `json:"-"`

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
		log.Error("Component type is required", zap.Error(err))
		return err
	}

	c.quitCh = make(chan struct{}, 1)
	c.Varz.StartTime = schema.Time(time.Now())
	guid, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}
	c.Varz.UUID = fmt.Sprintf("%d-%s", c.Varz.Index, guid)

	if c.Varz.Host == "" {
		host, err := localip.LocalIP()
		if err != nil {
			log.Error("error-getting-localIP", zap.Error(err))
			return err
		}

		port := test_util.NextAvailPort()

		c.Varz.Host = fmt.Sprintf("%s:%d", host, port)
	}

	if c.Varz.Credentials == nil || len(c.Varz.Credentials) != 2 {
		user, err := uuid.GenerateUUID()
		if err != nil {
			return err
		}
		password, err := uuid.GenerateUUID()
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

	return c.ListenAndServe()
}

func (c *VcapComponent) Register(mbusClient *nats.Conn) error {
	_, err := mbusClient.Subscribe("vcap.component.discover", func(msg *nats.Msg) {
		if msg.Reply == "" {
			log.Info("Received message with empty reply", zap.String("nats-msg-subject", msg.Subject))
			return
		}

		c.Varz.Uptime = c.Varz.StartTime.Elapsed()
		b, e := json.Marshal(c.Varz)
		if e != nil {
			log.Error("error-json-marshaling", zap.Error(e))
			return
		}

		err := mbusClient.Publish(msg.Reply, b)
		if err != nil {
			log.Error("error-publishing-registration", zap.Error(e))
		}
	})
	if err != nil {
		return err
	}

	b, e := json.Marshal(c.Varz)
	if e != nil {
		log.Error("error-json-marshaling", zap.Error(e))
		return e
	}

	err = mbusClient.Publish("vcap.component.announce", b)
	if err != nil {
		return err
	}

	log.Info(fmt.Sprintf("Component %s registered successfully", c.Varz.Type))
	return nil
}

func (c *VcapComponent) Stop() error {
	close(c.quitCh)
	if c.listener != nil {
		err := c.listener.Close()
		<-c.statusCh
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *VcapComponent) ListenAndServe() error {
	hs := http.NewServeMux()

	hs.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		c.Health.ServeHTTP(w, req)
	})

	hs.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		c.Health.ServeHTTP(w, req)
	})

	hs.HandleFunc("/varz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Connection", "close")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		enc := json.NewEncoder(w)
		c.UpdateVarz()
		// #nosec G104 - ignore errors when writing HTTP responses so we don't spam our logs during a DoS
		enc.Encode(c.Varz)
	})

	for path, marshaler := range c.InfoRoutes {
		m := marshaler
		hs.HandleFunc(path, func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Connection", "close")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			enc := json.NewEncoder(w)
			// #nosec G104 - ignore errors when writing HTTP responses so we don't spam our logs during a DoS
			enc.Encode(m)
		})
	}

	f := func(user, password string) bool {
		return user == c.Varz.Credentials[0] && password == c.Varz.Credentials[1]
	}

	s := &http.Server{
		Addr:         c.Varz.Host,
		Handler:      &BasicAuth{Handler: hs, Authenticator: f},
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	c.statusCh = make(chan error, 1)
	l, err := net.Listen("tcp", c.Varz.Host)
	if err != nil {
		c.statusCh <- err
		return err
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
	return nil
}
