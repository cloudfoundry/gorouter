package health_test

import (
	"fmt"
	"strconv"

	"github.com/cloudfoundry/gorouter/common/health"
	"github.com/cloudfoundry/gorouter/common/schema"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager"

	"encoding/json"
)

var _ = Describe("Varz", func() {

	It("contains expected keys", func() {
		varz := &health.Varz{}
		varz.LogCounts = schema.NewLogCounter()

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
		varz := &health.Varz{}
		varz.LogCounts = schema.NewLogCounter()

		infoMsg := lager.LogFormat{
			LogLevel: lager.INFO,
			Message:  "info-message",
		}
		varz.LogCounts.Log(infoMsg)

		bytes, _ := json.Marshal(varz)
		data := make(map[string]interface{})
		json.Unmarshal(bytes, &data)

		counts := data["log_counts"].(map[string]interface{})
		count := counts[strconv.Itoa(int(lager.INFO))]

		Expect(count).To(Equal(float64(1)))
	})

	Context("UniqueVarz", func() {
		It("marshals as a struct", func() {
			varz := &health.Varz{
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
			varz := &health.Varz{
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
