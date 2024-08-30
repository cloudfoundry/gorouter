package common

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"

	nats "github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
)

type TcpApp struct {
	mutex sync.Mutex

	port         uint16      // app listening port
	rPort        uint16      // router listening port
	urls         []route.Uri // host registered host name
	mbusClient   *nats.Conn
	tags         map[string]string
	listener     net.Listener
	handlers     []func(conn *test_util.HttpConn)
	stopped      bool
	routeService string
	GUID         string
}

func NewTcpApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, tags map[string]string, routeService string) *TcpApp {
	app := new(TcpApp)

	port := test_util.NextAvailPort()

	app.port = port
	app.rPort = rPort
	app.urls = urls
	app.mbusClient = mbusClient
	app.tags = tags
	app.routeService = routeService
	app.GUID, _ = uuid.GenerateUUID()

	return app
}

func (a *TcpApp) SetHandlers(handlers []func(conn *test_util.HttpConn)) {
	a.handlers = handlers
}

func (a *TcpApp) Urls() []route.Uri {
	return a.urls
}

func (a *TcpApp) SetRouteService(routeService string) {
	a.routeService = routeService
}

func (a *TcpApp) Endpoint() string {
	return fmt.Sprintf("http://%s:%d/", a.urls[0], a.rPort)
}

func (a *TcpApp) Listen() error {
	var err error
	a.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", a.port))

	if err != nil {
		return err
	}

	go func() {
		defer GinkgoRecover()
		for i := 0; i < len(a.handlers); i++ {
			if a.isStopped() {
				a.listener.Close()
				break
			}
			conn, err := a.listener.Accept()
			Expect(err).NotTo(HaveOccurred())

			a.handlers[i](test_util.NewHttpConn(conn))
		}

	}()
	return nil
}

func (a *TcpApp) RegisterAndListen() {
	a.Register()
	a.Listen()
}

func (a *TcpApp) Port() uint16 {
	return a.port
}

func (a *TcpApp) AppGUID() string {
	return a.GUID
}

func (a *TcpApp) TlsRegister(serverCertDomainSAN string) {
	a.TlsRegisterWithIndex(serverCertDomainSAN, 0)
}
func (a *TcpApp) TlsRegisterWithIndex(serverCertDomainSAN string, index int) {
	id, _ := uuid.GenerateUUID()
	rm := registerMessage{
		Host:                    "127.0.0.1",
		TlsPort:                 a.port,
		Port:                    a.port,
		Uris:                    a.urls,
		Tags:                    a.tags,
		Dea:                     "dea",
		App:                     a.GUID,
		PrivateInstanceIndex:    fmt.Sprintf("%d", index),
		StaleThresholdInSeconds: 1,

		RouteServiceUrl:     a.routeService,
		ServerCertDomainSAN: serverCertDomainSAN,
		PrivateInstanceId:   id,
	}

	b, _ := json.Marshal(rm)
	a.mbusClient.Publish("router.register", b)
}
func (a *TcpApp) Register() {
	id, _ := uuid.GenerateUUID()
	rm := registerMessage{
		Host:                    "127.0.0.1",
		Port:                    a.port,
		Uris:                    a.urls,
		Tags:                    a.tags,
		Dea:                     "dea",
		App:                     a.GUID,
		PrivateInstanceIndex:    "0",
		StaleThresholdInSeconds: 1,

		RouteServiceUrl:   a.routeService,
		PrivateInstanceId: id,
	}

	b, _ := json.Marshal(rm)
	a.mbusClient.Publish("router.register", b)
}

func (a *TcpApp) Unregister() {
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

	a.Stop()
}

func (a *TcpApp) Stop() {
	a.mutex.Lock()
	a.stopped = true
	if a.listener != nil {
		a.listener.Close()
	}
	a.mutex.Unlock()
}

func (a *TcpApp) isStopped() bool {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.stopped
}
