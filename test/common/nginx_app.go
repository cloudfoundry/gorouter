package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	nats "github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
)

type NginxApp struct {
	port         uint16      // app listening port
	rPort        uint16      // router listening port
	urls         []route.Uri // host registered host name
	mbusClient   *nats.Conn
	tags         map[string]string
	routeService string
	session      *gexec.Session
	configFile   *os.File
	GUID         string
}

func NewNginxApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, tags map[string]string, routeService string) *NginxApp {
	app := new(NginxApp)

	port := test_util.NextAvailPort()

	app.port = port
	app.rPort = rPort
	app.urls = urls
	app.mbusClient = mbusClient
	app.tags = tags
	app.routeService = routeService
	app.GUID, _ = uuid.GenerateUUID()

	_, testFile, _, _ := runtime.Caller(1)
	nginxAppDir := filepath.Join(filepath.Dir(filepath.Dir(testFile)), "test", "nginx-app")

	var err error
	app.configFile, err = os.CreateTemp("", "gorouter-ngninx-test-app")
	Expect(err).NotTo(HaveOccurred())
	t, err := template.ParseFiles(filepath.Join(nginxAppDir, "nginx.conf"))
	Expect(err).NotTo(HaveOccurred())

	err = t.Execute(app.configFile, map[string]interface{}{
		"Port":       port,
		"ServerRoot": filepath.Join(nginxAppDir, "public"),
	})
	Expect(err).NotTo(HaveOccurred())
	app.session, err = gexec.Start(exec.Command("nginx", "-c", app.configFile.Name()), GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())

	err = app.waitUntilNginxUp()
	Expect(err).NotTo(HaveOccurred())
	return app
}

func (a *NginxApp) waitUntilNginxUp() error {
	maxWait := 10
	for i := 0; i < maxWait; i++ {
		time.Sleep(500 * time.Millisecond)
		_, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", a.port))
		if err == nil {
			return nil
		}
	}

	return errors.New("Waited too long for Nginx to start")
}

func (a *NginxApp) Urls() []route.Uri {
	return a.urls
}

func (a *NginxApp) Register() {
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
	err := a.mbusClient.Publish("router.register", b)
	Expect(err).NotTo(HaveOccurred())
}

func (a *NginxApp) Stop() {
	a.session.Terminate()
	Eventually(a.session).Should(gexec.Exit())
	err := os.Remove(a.configFile.Name())
	Expect(err).NotTo(HaveOccurred())
}
