package common_test

import (
	"fmt"

	. "github.com/cloudfoundry/gorouter/common"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"encoding/json"

	steno "github.com/cloudfoundry/gosteno"
)

var _ = Describe("Varz", func() {

	It("contains expected keys", func() {
		varz := &Varz{}
		varz.LogCounts = NewLogCounter()

		bytes, err := json.Marshal(varz)
		Expect(err).ToNot(HaveOccurred())

		data := make(map[string]interface{})
		err = json.Unmarshal(bytes, &data)
		Expect(err).ToNot(HaveOccurred())

		members := []string{
			"type",
			"index",
			"host",
			"credentials",
			"start",
			"uuid",
			"uptime",
			"num_cores",
			"mem",
			"cpu",
			"log_counts",
		}

		_, ok := data["config"]
		Expect(ok).To(BeFalse(), "config should be omitted from /varz")

		for _, key := range members {
			_, ok = data[key]
			Expect(ok).To(BeTrue(), fmt.Sprintf("member %s not found", key))
		}
	})

	It("contains Log counts", func() {
		varz := &Varz{}
		varz.LogCounts = NewLogCounter()

		varz.LogCounts.AddRecord(&steno.Record{Level: steno.LOG_INFO})

		bytes, _ := json.Marshal(varz)
		data := make(map[string]interface{})
		json.Unmarshal(bytes, &data)

		counts := data["log_counts"].(map[string]interface{})
		count := counts["info"]

		Expect(count).To(Equal(float64(1)))
	})

	Context("UniqueVarz", func() {
		It("marshals as a struct", func() {
			varz := &Varz{
				UniqueVarz: struct {
					Type  string `json:"my_type"`
					Index int    `json:"my_index"`
				}{
					Type:  "Router",
					Index: 1,
				},
			}

			bytes, _ := json.Marshal(varz)
			data := make(map[string]interface{})
			json.Unmarshal(bytes, &data)

			Expect(data["my_type"]).To(Equal("Router"))
			Expect(data["my_index"]).To(Equal(float64(1)))
		})

		It("marshals as a map", func() {
			varz := &Varz{
				UniqueVarz: map[string]interface{}{"my_type": "Dea", "my_index": 1},
			}
			bytes, _ := json.Marshal(varz)
			data := make(map[string]interface{})
			json.Unmarshal(bytes, &data)

			Expect(data["my_type"]).To(Equal("Dea"))
			Expect(data["my_index"]).To(Equal(float64(1)))
		})
	})
})
