package uuid_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/gorouter/common/uuid"
)

var _ = Describe("UUID", func() {
	It("creates a uuid", func() {
		uuid, err := uuid.GenerateUUID()
		Expect(err).ToNot(HaveOccurred())
		Expect(uuid).To(HaveLen(36))
	})
})
