package stats_test

import (
	. "code.cloudfoundry.org/gorouter/stats"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"time"
)

var _ = Describe("ActiveApps", func() {
	var activeApps *ActiveApps

	BeforeEach(func() {
		activeApps = NewActiveApps()
	})

	It("marks application ids active", func() {
		activeApps.Mark("a", time.Unix(1, 0))
		apps := activeApps.ActiveSince(time.Unix(1, 0))
		Expect(apps).To(HaveLen(1))
	})

	It("marks existing applications", func() {
		activeApps.Mark("b", time.Unix(1, 0))
		apps := activeApps.ActiveSince(time.Unix(1, 0))
		Expect(apps).To(HaveLen(1))

		activeApps.Mark("b", time.Unix(2, 0))
		apps = activeApps.ActiveSince(time.Unix(1, 0))
		Expect(apps).To(HaveLen(1))
	})

	It("trims aging application ids", func() {
		for i, x := range []string{"a", "b", "c"} {
			activeApps.Mark(x, time.Unix(int64(i+1), 0))
		}
		apps := activeApps.ActiveSince(time.Unix(0, 0))
		Expect(apps).To(HaveLen(3))

		activeApps.Trim(time.Unix(1, 0))
		apps = activeApps.ActiveSince(time.Unix(0, 0))
		Expect(apps).To(HaveLen(2))

		activeApps.Trim(time.Unix(2, 0))
		apps = activeApps.ActiveSince(time.Unix(0, 0))
		Expect(apps).To(HaveLen(1))

		activeApps.Trim(time.Unix(3, 0))
		apps = activeApps.ActiveSince(time.Unix(0, 0))
		Expect(apps).To(HaveLen(0))
	})

	It("returns application ids active since a point in time", func() {
		activeApps.Mark("a", time.Unix(1, 0))
		Expect(activeApps.ActiveSince(time.Unix(1, 0))).To(Equal([]string{"a"}))
		Expect(activeApps.ActiveSince(time.Unix(3, 0))).To(Equal([]string{}))
		Expect(activeApps.ActiveSince(time.Unix(5, 0))).To(Equal([]string{}))

		activeApps.Mark("b", time.Unix(3, 0))
		Expect(activeApps.ActiveSince(time.Unix(1, 0))).To(Equal([]string{"b", "a"}))
		Expect(activeApps.ActiveSince(time.Unix(3, 0))).To(Equal([]string{"b"}))
		Expect(activeApps.ActiveSince(time.Unix(5, 0))).To(Equal([]string{}))
	})
})
