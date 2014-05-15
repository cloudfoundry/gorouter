package util_test

import (
	. "github.com/cloudfoundry/gorouter/util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
)

var _ = Describe("Pidfile", func() {
	var path string
	var pidfile string

	BeforeEach(func() {
		x, err := ioutil.TempDir("", "PidFileSuite")
		Ω(err).ShouldNot(HaveOccurred())

		path = x
		pidfile = filepath.Join(path, "pidfile")
	})

	AfterEach(func() {
		err := os.RemoveAll(path)
		Ω(err).ShouldNot(HaveOccurred())
	})

	assertPidfileNonzero := func() {
		x, err := ioutil.ReadFile(pidfile)
		Ω(err).ShouldNot(HaveOccurred())

		y, err := strconv.Atoi(string(x))
		Ω(err).ShouldNot(HaveOccurred())
		Ω(y).ShouldNot(Equal(0))
	}

	It("writes a pid file", func() {
		err := WritePidFile(pidfile)
		Ω(err).ShouldNot(HaveOccurred())

		assertPidfileNonzero()
	})

	It("overwrites the pid file", func() {
		err := ioutil.WriteFile(pidfile, []byte("0"), 0644)
		Ω(err).ShouldNot(HaveOccurred())

		err = WritePidFile(pidfile)
		Ω(err).ShouldNot(HaveOccurred())

		assertPidfileNonzero()
	})

	Context("when path dows not exist", func() {
		It("returns an error", func() {
			err := os.RemoveAll(path)
			Ω(err).ShouldNot(HaveOccurred())

			err = WritePidFile(pidfile)
			Ω(err).Should(HaveOccurred())
		})
	})
})
