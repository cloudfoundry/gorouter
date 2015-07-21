package common_test

import (
	. "github.com/cloudfoundry/gorouter/common"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"encoding/json"
	"fmt"
	"time"
)

var _ = Describe("Durations", func() {
	Context("Duration", func() {
		It("supports JSON", func() {
			d := Duration(123456)
			var i interface{} = &d

			_, ok := i.(json.Marshaler)
			Expect(ok).To(BeTrue())

			_, ok = i.(json.Unmarshaler)
			Expect(ok).To(BeTrue())
		})

		It("marshals JSON", func() {
			d := Duration(time.Hour*36 + time.Second*10)
			b, err := json.Marshal(d)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(b)).To(Equal(`"1d:12h:0m:10s"`))
		})

		It("unmarshals JSON", func() {
			d := Duration(time.Hour*36 + time.Second*20)
			b, err := json.Marshal(d)
			Expect(err).ToNot(HaveOccurred())

			var dd Duration
			dd.UnmarshalJSON(b)
			Expect(dd).To(Equal(d))
		})
	})

	Context("Time", func() {
		It("marshals JSON", func() {
			n := time.Now()
			f := "2006-01-02 15:04:05 -0700"

			t := Time(n)
			b, e := json.Marshal(t)
			Expect(e).ToNot(HaveOccurred())
			Expect(string(b)).To(Equal(fmt.Sprintf(`"%s"`, n.Format(f))))
		})

		It("unmarshals JSON", func() {
			t := Time(time.Unix(time.Now().Unix(), 0)) // The precision of Time is 'second'
			b, err := json.Marshal(t)
			Expect(err).ToNot(HaveOccurred())

			var tt Time
			err = tt.UnmarshalJSON(b)
			Expect(err).ToNot(HaveOccurred())
			Expect(tt).To(Equal(t))
		})

	})

	Describe("Unix To Time", func() {
		Context("when the unix time is valid", func() {
			It("converts unix time stamp to time struct", func() {
				unixTime := "1437497865"

				tm, err := UnixToTime(unixTime)
				Expect(err).ToNot(HaveOccurred())
				expectedTime, err := time.Parse(time.UnixDate, "Tue Jul 21 16:57:45 UTC 2015")
				Expect(err).ToNot(HaveOccurred())

				Expect(tm.Sub(expectedTime)).To(Equal(time.Duration(0)))
			})
		})

		Context("when the unix time is invalid", func() {
			It("returns an error", func() {
				unixTime := "invalid time string"

				_, err := UnixToTime(unixTime)
				Expect(err).To(HaveOccurred())
			})
		})
	})
})
