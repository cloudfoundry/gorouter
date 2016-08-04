package main_test

import (
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

var rssPath string
var rssCommand = func(args ...string) *exec.Cmd {
	command := exec.Command(rssPath, args...)
	return command
}

const keyPath = "fixtures/key"

func TestRssCli(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RSS Cli Suite")
}

var _ = SynchronizedBeforeSuite(func() []byte {

	createDefaultKey()

	cliPath, err := gexec.Build("code.cloudfoundry.org/gorouter/test_util/rss")
	Expect(err).NotTo(HaveOccurred())
	return []byte(cliPath)
}, func(cliPath []byte) {
	rssPath = string(cliPath)
})

var _ = SynchronizedAfterSuite(func() {
}, func() {
	removeDefaultKey()
	gexec.CleanupBuildArtifacts()
})

func createDefaultKey() {
	keyDir := getKeyDir()

	if !isDirExist(keyDir) {
		err := os.Mkdir(keyDir, os.ModePerm)
		Expect(err).NotTo(HaveOccurred())
	}

	copyFile(keyPath, keyDir+"/key")
}

func removeDefaultKey() {
	keyDir := getKeyDir()
	err := os.RemoveAll(keyDir)
	Expect(err).NotTo(HaveOccurred())
}

func getKeyDir() string {
	usr, err := user.Current()
	Expect(err).NotTo(HaveOccurred())
	return usr.HomeDir + "/.rss"
}

func isDirExist(dir string) bool {
	_, err := os.Stat(dir)
	return !os.IsNotExist(err)
}

func copyFile(src, dest string) {
	data, err := ioutil.ReadFile(src)
	Expect(err).NotTo(HaveOccurred())
	writeToFile(data, dest)
}

func writeToFile(data []byte, fileName string) {
	var file *os.File
	var err error
	file, err = os.Create(fileName)
	Expect(err).NotTo(HaveOccurred())

	_, err = file.Write(data)
	Expect(err).NotTo(HaveOccurred())

	err = file.Close()
	Expect(err).NotTo(HaveOccurred())
}
