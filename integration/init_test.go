package integration

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"encoding/asn1"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"code.cloudfoundry.org/gorouter/test_util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"

	"testing"
)

var (
	gorouterPath string
	oauthServer  *ghttp.Server
	testAssets   string
	caCertsPath  string
)

type Path struct {
	Gorouter string `json:"gorouter"`
	Test     string `json:"test"`
}

var _ = SynchronizedBeforeSuite(func() []byte {
	path, err := gexec.Build("code.cloudfoundry.org/gorouter", "-race")
	Expect(err).ToNot(HaveOccurred())
	test, err := ioutil.TempDir("", "test")
	Expect(err).ToNot(HaveOccurred())

	pathStruct := Path{path, test}
	reqBodyBytes := new(bytes.Buffer)
	json.NewEncoder(reqBodyBytes).Encode(pathStruct)
	return []byte(reqBodyBytes.Bytes())
}, func(data []byte) {
	res := Path{}
	json.Unmarshal([]byte(string(data)), &res)
	gorouterPath = res.Gorouter
	testAssets = res.Test
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
	SetDefaultConsistentlyDuration(1 * time.Second)
	SetDefaultConsistentlyPollingInterval(10 * time.Millisecond)
	oauthServer = setupTLSServer()
	oauthServer.HTTPTestServer.StartTLS()
})

var _ = SynchronizedAfterSuite(func() {
	if oauthServer != nil {
		oauthServer.Close()
	}
}, func() {
	os.RemoveAll(testAssets)
	gexec.CleanupBuildArtifacts()
})

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

func setupTLSServer() *ghttp.Server {
	oauthCertName := test_util.CertNames{
		CommonName: "oauth-server",
		SANs: test_util.SubjectAltNames{
			IP: "127.0.0.1",
		},
	}
	certChain := test_util.CreateSignedCertWithRootCA(oauthCertName)
	caCertsPath = certChain.WriteCACertToDir(testAssets)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certChain.TLSCert()},
		CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA},
	}

	server := ghttp.NewUnstartedServer()
	server.HTTPTestServer.TLS = tlsConfig
	server.AllowUnhandledRequests = true
	server.UnhandledRequestStatusCode = http.StatusOK

	// generate publicKey
	reader := rand.Reader
	bitSize := 2048
	key, err := rsa.GenerateKey(reader, bitSize)
	Expect(err).NotTo(HaveOccurred())
	publicKey := key.PublicKey
	pkBytes, err := asn1.Marshal(publicKey)
	Expect(err).NotTo(HaveOccurred())

	data := fmt.Sprintf("{\"alg\":\"rsa\", \"value\":\"%s\"}", pkBytes)
	server.RouteToHandler("GET", "/token_key",
		ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/token_key"),
			ghttp.RespondWith(http.StatusOK, data)),
	)
	server.RouteToHandler("POST", "/oauth/token",
		func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			jsonBytes := []byte(`{"access_token":"some-token", "expires_in":10}`)
			w.Write(jsonBytes)
		})
	return server
}
