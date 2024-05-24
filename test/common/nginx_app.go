package common

import (
	"encoding/json"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"code.cloudfoundry.org/gorouter/common/uuid"
	"code.cloudfoundry.org/gorouter/route"
	"code.cloudfoundry.org/gorouter/test_util"
	nats "github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

type NginxApp struct {
	mutex sync.Mutex

	port         uint16      // app listening port
	rPort        uint16      // router listening port
	urls         []route.Uri // host registered host name
	mbusClient   *nats.Conn
	tags         map[string]string
	stopped      bool
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

	t.Execute(app.configFile, map[string]interface{}{
		"Port":       port,
		"ServerRoot": filepath.Join(nginxAppDir, "public"),
	})
	app.session, err = gexec.Start(exec.Command("nginx", "-c", app.configFile.Name()), GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())

	return app
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
	a.mbusClient.Publish("router.register", b)
}

func (a *NginxApp) Stop() {
	a.session.Terminate()
	Eventually(a.session).Should(gexec.Exit())
	os.Remove(a.configFile.Name())
}
