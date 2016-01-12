package common_test

import (
	"encoding/json"
	"strconv"

	. "github.com/cloudfoundry/gorouter/common"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager"
)

var _ = Describe("LogCounter", func() {
	var (
		infoMessage  []byte
		errorMessage []byte
	)

	BeforeEach(func() {
		infoMessage = []byte("info-message")
		errorMessage = []byte("error-message")
	})

	It("counts the number of records", func() {
		counter := NewLogCounter()
		counter.Log(lager.INFO, infoMessage)
		Expect(counter.GetCount(strconv.Itoa(int(lager.INFO)))).To(Equal(1))

		counter.Log(lager.INFO, infoMessage)
		Expect(counter.GetCount(strconv.Itoa(int(lager.INFO)))).To(Equal(2))
	})

	It("counts all log levels", func() {
		counter := NewLogCounter()
		counter.Log(lager.INFO, infoMessage)
		Expect(counter.GetCount(strconv.Itoa(int(lager.INFO)))).To(Equal(1))

		counter.Log(lager.ERROR, errorMessage)
		Expect(counter.GetCount(strconv.Itoa(int(lager.ERROR)))).To(Equal(1))
	})

	It("marshals the set of counts", func() {
		counter := NewLogCounter()
		counter.Log(lager.INFO, infoMessage)
		counter.Log(lager.ERROR, errorMessage)

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
