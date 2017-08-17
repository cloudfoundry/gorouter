package common

import (
	"io/ioutil"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/localip"
	"github.com/nats-io/nats"
	. "github.com/onsi/gomega"

	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type TestApp struct {
	mutex sync.Mutex

	port         uint16      // app listening port
	rPort        uint16      // router listening port
	urls         []route.Uri // host registered host name
	mbusClient   *nats.Conn
	tags         map[string]string
	mux          *http.ServeMux
	stopped      bool
	routeService string
}

func NewTestApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, tags map[string]string, routeService string) *TestApp {
	app := new(TestApp)

	port, _ := localip.LocalPort()

	app.port = port
	app.rPort = rPort
	app.urls = urls
	app.mbusClient = mbusClient
	app.tags = tags
	app.routeService = routeService

	app.mux = http.NewServeMux()

	return app
}

func (a *TestApp) AddHandler(path string, handler func(http.ResponseWriter, *http.Request)) {
	a.mux.HandleFunc(path, handler)
}

func (a *TestApp) Urls() []route.Uri {
	return a.urls
}

func (a *TestApp) Endpoint() string {
	return fmt.Sprintf("http://%s:%d/", a.urls[0], a.rPort)
}

func (a *TestApp) TlsListen(serverPEM, serverKey []byte) error {
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", a.port),
		Handler: a.mux,
	}
	certFile, err := ioutil.TempFile("", "cert")
	if err != nil {
		return err
	}

	keyFile, err := ioutil.TempFile("", "key")
	if err != nil {
		return err
	}

	_, err = certFile.Write(serverPEM)
	if err != nil {
		return err
	}
	_, err = keyFile.Write(serverKey)
	if err != nil {
		return err
	}
	certFile.Close()
	keyFile.Close()

	go server.ListenAndServeTLS(certFile.Name(), keyFile.Name())
	return nil
}

func (a *TestApp) RegisterAndListen() {
	a.Register()
	a.Listen()
}

func (a *TestApp) Listen() {
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", a.port),
		Handler: a.mux,
	}
	go server.ListenAndServe()
}

func (a *TestApp) RegisterRepeatedly(duration time.Duration) {
	a.start()
	for {
		if a.isStopped() {
			break
		}
		a.Register()
		time.Sleep(duration)
	}
}

func (a *TestApp) Port() uint16 {
	return a.port
}

func (a *TestApp) TlsRegister(privateInstanceId string) {
	rm := registerMessage{
		Host:    "127.0.0.1",
		TlsPort: a.port,
		Port:    a.port,
		Uris:    a.urls,
		Tags:    a.tags,
		Dea:     "dea",
		App:     "0",
		StaleThresholdInSeconds: 1,

		RouteServiceUrl:   a.routeService,
		PrivateInstanceId: privateInstanceId,
	}

	b, _ := json.Marshal(rm)
	a.mbusClient.Publish("router.register", b)
}
func (a *TestApp) Register() {
	uuid, _ := uuid.GenerateUUID()
	rm := registerMessage{
		Host: "127.0.0.1",
		Port: a.port,
		Uris: a.urls,
		Tags: a.tags,
		Dea:  "dea",
		App:  "0",
		StaleThresholdInSeconds: 1,

		RouteServiceUrl:   a.routeService,
		PrivateInstanceId: uuid,
	}

	b, _ := json.Marshal(rm)
	a.mbusClient.Publish("router.register", b)
}

func (a *TestApp) Unregister() {
	rm := registerMessage{
		Host: "127.0.0.1",
		Port: a.port,
		Uris: a.urls,
		Tags: nil,
		Dea:  "dea",
		App:  "0",
	}

	b, _ := json.Marshal(rm)
	a.mbusClient.Publish("router.unregister", b)

	a.stop()
}

func (a *TestApp) VerifyAppStatus(status int) {
	EventuallyWithOffset(1, func() error {
		return a.CheckAppStatus(status)
	}).ShouldNot(HaveOccurred())
}

func (a *TestApp) CheckAppStatusWithPath(status int, path string) error {
	for _, url := range a.urls {
		uri := fmt.Sprintf("http://%s:%d/%s", url, a.rPort, path)
		testReq, err := http.NewRequest("GET", uri, nil)
		if err != nil {
			return err
		}
		testClient := http.Client{}
		testClient.Timeout = 90 * time.Second
		resp, err := testClient.Do(testReq)
		if err != nil {
			return err
		}

		if resp.StatusCode != status {
			return errors.New(fmt.Sprintf("expected status code %d, got %d", status, resp.StatusCode))
		}
	}

	return nil
}

func (a *TestApp) CheckAppStatus(status int) error {
	for _, url := range a.urls {
		uri := fmt.Sprintf("http://%s:%d", url, a.rPort)
		resp, err := http.Get(uri)
		if err != nil {
			return err
		}

		if resp.StatusCode != status {
			return errors.New(fmt.Sprintf("expected status code %d, got %d", status, resp.StatusCode))
		}
	}

	return nil
}

func (a *TestApp) start() {
	a.mutex.Lock()
	a.stopped = false
	a.mutex.Unlock()
}

func (a *TestApp) stop() {
	a.mutex.Lock()
	a.stopped = true
	a.mutex.Unlock()
}

func (a *TestApp) isStopped() bool {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.stopped
}

type registerMessage struct {
	Host                    string            `json:"host"`
	Port                    uint16            `json:"port"`
	TlsPort                 uint16            `json:"tls_port"`
	Uris                    []route.Uri       `json:"uris"`
	Tags                    map[string]string `json:"tags"`
	Dea                     string            `json:"dea"`
	App                     string            `json:"app"`
	StaleThresholdInSeconds int               `json:"stale_threshold_in_seconds"`

	RouteServiceUrl   string `json:"route_service_url"`
	PrivateInstanceId string `json:"private_instance_id"`
}
