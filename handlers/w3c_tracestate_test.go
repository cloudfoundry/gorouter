package handlers_test

import (
	"code.cloudfoundry.org/gorouter/handlers"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("W3CTracestate", func() {
	Context("when creating a new W3CTracestate", func() {
		Context("when there is no tenantID", func() {
			It("should create the entry without an @ symbol", func() {
				tenantID := ""
				parentID := []byte("cf")

				tracestate := handlers.NewW3CTracestate(tenantID, parentID)

				Expect(tracestate).To(HaveLen(1))
				Expect(tracestate[0].Key).To(Equal("gorouter"))
				Expect(tracestate[0].Val).To(Equal("6366"))
				Expect(tracestate[0].String()).To(Equal("gorouter=6366"))
			})
		})

		Context("when there is a tenantID", func() {
			It("should create the entry with @ symbol and the tenantID", func() {
				tenantID := "tid"
				parentID := []byte("cf")

				tracestate := handlers.NewW3CTracestate(tenantID, parentID)

				Expect(tracestate).To(HaveLen(1))
				Expect(tracestate[0].Key).To(Equal("tid@gorouter"))
				Expect(tracestate[0].Val).To(Equal("6366"))
				Expect(tracestate[0].String()).To(Equal("tid@gorouter=6366"))
			})
		})
	})

	Context("when updating an existing W3CTracestate", func() {
		Context("when there is no tenantID", func() {
			Context("when the W3CTracestate is empty", func() {
				It("should create the entry without an @ symbol", func() {
					tenantID := ""
					parentID := []byte("cf")

					tracestate := make(handlers.W3CTracestate, 0)
					tracestate = tracestate.Next(tenantID, parentID)

					Expect(tracestate).To(HaveLen(1))
					Expect(tracestate[0].Key).To(Equal("gorouter"))
					Expect(tracestate[0].Val).To(Equal("6366"))
					Expect(tracestate[0].String()).To(Equal("gorouter=6366"))
				})
			})

			Context("when the W3CTracestate is not empty", func() {
				It("should create the entry without an @ symbol", func() {
					tenantID := ""
					parentID := []byte("cf")

					ts := handlers.W3CTracestate{
						handlers.W3CTracestateEntry{Key: "congo", Val: "t61rcWkgMzE"},
					}

					ts = ts.Next(tenantID, parentID)

					Expect(ts).To(HaveLen(2))

					Expect(ts[0].String()).To(Equal("congo=t61rcWkgMzE"))
					Expect(ts[1].String()).To(Equal("gorouter=6366"))

					Expect(ts.String()).To(Equal("gorouter=6366,congo=t61rcWkgMzE"))
				})

				Context("when a matching tracestate entry already exists", func() {
					It("should keep the most recent entry with the tenant id", func() {
						tenantID := ""
						parentID := []byte("cf")

						ts := handlers.W3CTracestate{
							handlers.W3CTracestateEntry{Key: "rojo", Val: "00f067aa0ba902b7"},
							handlers.W3CTracestateEntry{Key: "gorouter", Val: "prev"},
							handlers.W3CTracestateEntry{Key: "congo", Val: "t61rcWkgMzE"},
						}

						ts = ts.Next(tenantID, parentID)

						Expect(ts).To(HaveLen(3))

						Expect(ts[0].String()).To(Equal("rojo=00f067aa0ba902b7"))
						Expect(ts[1].String()).To(Equal("congo=t61rcWkgMzE"))
						Expect(ts[2].String()).To(Equal("gorouter=6366"))

						Expect(ts.String()).To(
							Equal("gorouter=6366,congo=t61rcWkgMzE,rojo=00f067aa0ba902b7"),
						)
					})

					It("should persist gorouters with different tenantIDs", func() {
						tenantID := ""
						parentID := []byte("cf")

						ts := handlers.W3CTracestate{
							handlers.W3CTracestateEntry{Key: "gorouter", Val: "a"},
							handlers.W3CTracestateEntry{Key: "other@gorouter", Val: "b"},
						}

						ts = ts.Next(tenantID, parentID)

						Expect(ts).To(HaveLen(2))

						Expect(ts[0].String()).To(Equal("other@gorouter=b"))
						Expect(ts[1].String()).To(Equal("gorouter=6366"))

						Expect(ts.String()).To(
							Equal("gorouter=6366,other@gorouter=b"),
						)
					})
				})
			})
		})

		Context("when there is a tenantID", func() {
			Context("when the W3CTracestate is empty", func() {
				It("should create the entry with @ symbol and the tenantID", func() {
					tenantID := "tid"
					parentID := []byte("cf")

					tracestate := make(handlers.W3CTracestate, 0)
					tracestate = tracestate.Next(tenantID, parentID)

					Expect(tracestate).To(HaveLen(1))
					Expect(tracestate[0].Key).To(Equal("tid@gorouter"))
					Expect(tracestate[0].Val).To(Equal("6366"))
					Expect(tracestate[0].String()).To(Equal("tid@gorouter=6366"))
				})
			})

			Context("when the W3CTracestate is not empty", func() {
				It("should create the entry with @ symbol and the tenantID", func() {
					tenantID := "tid"
					parentID := []byte("cf")

					ts := handlers.W3CTracestate{
						handlers.W3CTracestateEntry{Key: "congo", Val: "t61rcWkgMzE"},
					}

					ts = ts.Next(tenantID, parentID)

					Expect(ts).To(HaveLen(2))

					Expect(ts[0].String()).To(Equal("congo=t61rcWkgMzE"))
					Expect(ts[1].String()).To(Equal("tid@gorouter=6366"))

					Expect(ts.String()).To(Equal("tid@gorouter=6366,congo=t61rcWkgMzE"))
				})

				Context("when a gorouter tracestate entry already exists", func() {
					It("should keep the most recent entry with the tenant id", func() {
						tenantID := "tid"
						parentID := []byte("cf")

						ts := handlers.W3CTracestate{
							handlers.W3CTracestateEntry{Key: "rojo", Val: "00f067aa0ba902b7"},
							handlers.W3CTracestateEntry{Key: "tid@gorouter", Val: "prev"},
							handlers.W3CTracestateEntry{Key: "congo", Val: "t61rcWkgMzE"},
						}

						ts = ts.Next(tenantID, parentID)

						Expect(ts).To(HaveLen(3))

						Expect(ts[0].String()).To(Equal("rojo=00f067aa0ba902b7"))
						Expect(ts[1].String()).To(Equal("congo=t61rcWkgMzE"))
						Expect(ts[2].String()).To(Equal("tid@gorouter=6366"))

						Expect(ts.String()).To(
							Equal("tid@gorouter=6366,congo=t61rcWkgMzE,rojo=00f067aa0ba902b7"),
						)
					})

					It("should persist gorouters with different tenantIDs", func() {
						tenantID := "tid"
						parentID := []byte("cf")

						ts := handlers.W3CTracestate{
							handlers.W3CTracestateEntry{Key: "gorouter", Val: "a"},
							handlers.W3CTracestateEntry{Key: "other@gorouter", Val: "b"},
						}

						ts = ts.Next(tenantID, parentID)

						Expect(ts).To(HaveLen(3))

						Expect(ts[0].String()).To(Equal("gorouter=a"))
						Expect(ts[1].String()).To(Equal("other@gorouter=b"))
						Expect(ts[2].String()).To(Equal("tid@gorouter=6366"))

						Expect(ts.String()).To(
							Equal("tid@gorouter=6366,other@gorouter=b,gorouter=a"),
						)
					})
				})
			})
		})
	})
})
