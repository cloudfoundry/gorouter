package uuid_test

import (
	"code.cloudfoundry.org/gorouter/common/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("UUID", func() {
	It("creates a uuid", func() {
		uuid, err := uuid.GenerateUUID()
		Expect(err).ToNot(HaveOccurred())
		Expect(uuid).To(HaveLen(36))
	})
})
