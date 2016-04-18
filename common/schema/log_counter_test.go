package schema_test

import (
	"encoding/json"
	"strconv"

	"github.com/cloudfoundry/gorouter/common/schema"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager"
)

var _ = Describe("LogCounter", func() {
	var (
		infoMsg lager.LogFormat
		errMsg  lager.LogFormat
	)

	BeforeEach(func() {
		infoMsg = lager.LogFormat{
			LogLevel: lager.INFO,
			Message:  "info-message",
		}

		errMsg = lager.LogFormat{
			LogLevel: lager.ERROR,
			Message:  "error-message",
		}
	})

	It("counts the number of records", func() {
		counter := schema.NewLogCounter()
		counter.Log(infoMsg)
		Expect(counter.GetCount(strconv.Itoa(int(lager.INFO)))).To(Equal(1))

		counter.Log(infoMsg)
		Expect(counter.GetCount(strconv.Itoa(int(lager.INFO)))).To(Equal(2))
	})

	It("counts all log levels", func() {
		counter := schema.NewLogCounter()
		counter.Log(infoMsg)
		Expect(counter.GetCount(strconv.Itoa(int(lager.INFO)))).To(Equal(1))

		counter.Log(errMsg)
		Expect(counter.GetCount(strconv.Itoa(int(lager.ERROR)))).To(Equal(1))
	})

	It("marshals the set of counts", func() {
		counter := schema.NewLogCounter()
		counter.Log(infoMsg)
		counter.Log(errMsg)

		b, e := counter.MarshalJSON()
		Expect(e).ToNot(HaveOccurred())

		v := map[string]int{}
		e = json.Unmarshal(b, &v)
		Expect(e).ToNot(HaveOccurred())
		Expect(v).To(HaveLen(2))
		Expect(v[strconv.Itoa(int(lager.INFO))]).To(Equal(1))
		Expect(v[strconv.Itoa(int(lager.ERROR))]).To(Equal(1))
	})
})
