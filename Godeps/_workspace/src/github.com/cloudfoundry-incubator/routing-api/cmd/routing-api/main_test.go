package main_test

import (
	"os/exec"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gexec"
)

var _ = Describe("Main", func() {
	It("exits 1 if no config file is provided", func() {
		session := RoutingApi()
		Eventually(session).Should(Exit(1))
	})

	It("exits 1 if the uaa_verification_key is not a valid PEM format", func() {
		session := RoutingApi("-config=../example_config/bad_uaa_verification_key.yml")
		Eventually(session).Should(Exit(1))
	})
})

func RoutingApi(args ...string) *Session {
	path, err := Build("github.com/cloudfoundry-incubator/routing-api/cmd/routing-api")
	Expect(err).NotTo(HaveOccurred())

	session, err := Start(exec.Command(path, args...), GinkgoWriter, GinkgoWriter)
	Expect(err).ToNot(HaveOccurred())

	return session
}
