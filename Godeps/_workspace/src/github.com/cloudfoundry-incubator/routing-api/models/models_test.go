package models_test

import (
	. "github.com/cloudfoundry-incubator/routing-api/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Models", func() {
	Describe("ModificationTag", func() {
		var tag ModificationTag

		BeforeEach(func() {
			tag = ModificationTag{"guid1", 5}
		})

		Describe("Increment", func() {
			BeforeEach(func() {
				tag.Increment()
			})

			It("Increments the index", func() {
				Expect(tag.Index).To(Equal(uint32(6)))
			})
		})

		Describe("SucceededBy", func() {
			var tag2 ModificationTag

			Context("when the guid is the different", func() {
				BeforeEach(func() {
					tag2 = ModificationTag{"guid5", 0}
				})
				It("new tag should succeed", func() {
					Expect(tag.SucceededBy(&tag2)).To(BeTrue())
				})
			})

			Context("when the guid is the same", func() {

				Context("when the index is the same as the original tag", func() {
					BeforeEach(func() {
						tag2 = ModificationTag{"guid1", 5}
					})

					It("new tag should not succeed", func() {
						Expect(tag.SucceededBy(&tag2)).To(BeFalse())
					})

				})

				Context("when the index is less than original tag Index", func() {

					BeforeEach(func() {
						tag2 = ModificationTag{"guid1", 4}
					})

					It("new tag should not succeed", func() {
						Expect(tag.SucceededBy(&tag2)).To(BeFalse())
					})
				})

				Context("when the index is greater than original tag Index", func() {
					BeforeEach(func() {
						tag2 = ModificationTag{"guid1", 6}
					})

					It("new tag should succeed", func() {
						Expect(tag.SucceededBy(&tag2)).To(BeTrue())
					})

				})

			})

		})
	})

	Describe("RouterGroup", func() {
		var rg RouterGroup

		Describe("Validate", func() {
			It("succeeds for valid router group", func() {
				rg = RouterGroup{
					Name:            "router-group-1",
					Type:            "tcp",
					ReservablePorts: "1025-2025",
				}
				err := rg.Validate()
				Expect(err).NotTo(HaveOccurred())
			})

			It("fails for missing type", func() {
				rg = RouterGroup{
					Name:            "router-group-1",
					ReservablePorts: "10-20",
				}
				err := rg.Validate()
				Expect(err).To(HaveOccurred())
			})

			It("fails for missing name", func() {
				rg = RouterGroup{
					Type:            "tcp",
					ReservablePorts: "10-20",
				}
				err := rg.Validate()
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("ReservablePorts", func() {
		var ports ReservablePorts

		Describe("Validate", func() {

			It("succeeds for valid reservable ports", func() {
				ports = "6001,6005,6010-6020,6021-6030"
				err := ports.Validate()
				Expect(err).NotTo(HaveOccurred())
			})

			It("fails for overlapping ranges", func() {
				ports = "6010-6020,6020-6030"
				err := ports.Validate()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("Overlapping values: [6010-6020] and [6020-6030]"))
			})

			It("fails for overlapping values", func() {
				ports = "6001,6001,6002,6003,6003,6004"
				err := ports.Validate()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("Overlapping values: 6001 and 6001"))
			})

			It("fails for invalid reservable ports", func() {
				ports = "foo!"
				err := ports.Validate()
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("Parse", func() {
			It("validates a single unsigned integer", func() {
				ports = "9999"
				r, err := ports.Parse()
				Expect(err).NotTo(HaveOccurred())

				Expect(len(r)).To(Equal(1))
				start, end := r[0].Endpoints()
				Expect(start).To(Equal(uint64(9999)))
				Expect(end).To(Equal(uint64(9999)))
			})

			It("validates multiple integers", func() {
				ports = "9999,1111,2222"
				r, err := ports.Parse()
				Expect(err).NotTo(HaveOccurred())
				Expect(len(r)).To(Equal(3))

				expected := []uint64{9999, 1111, 2222}
				for i := 0; i < len(r); i++ {
					start, end := r[i].Endpoints()
					Expect(start).To(Equal(expected[i]))
					Expect(end).To(Equal(expected[i]))
				}
			})

			It("validates a range", func() {
				ports = "10241-10249"
				r, err := ports.Parse()
				Expect(err).NotTo(HaveOccurred())

				Expect(len(r)).To(Equal(1))
				start, end := r[0].Endpoints()
				Expect(start).To(Equal(uint64(10241)))
				Expect(end).To(Equal(uint64(10249)))
			})

			It("validates a list of ranges and integers", func() {
				ports = "6001-6010,6020-6022,6045,6050-6060"
				r, err := ports.Parse()
				Expect(err).NotTo(HaveOccurred())

				Expect(len(r)).To(Equal(4))
				expected := []uint64{6001, 6010, 6020, 6022, 6045, 6045, 6050, 6060}
				for i := 0; i < len(r); i++ {
					start, end := r[i].Endpoints()
					Expect(start).To(Equal(expected[2*i]))
					Expect(end).To(Equal(expected[2*i+1]))
				}
			})

			It("errors on range with 3 dashes", func() {
				ports = "10-999-1000"
				_, err := ports.Parse()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("range (10-999-1000) has too many '-' separators"))
			})

			It("errors on a negative integer", func() {
				ports = "-9999"
				_, err := ports.Parse()
				Expect(err).To(HaveOccurred())
			})

			It("errors on a incomplete range", func() {
				ports = "1030-"
				_, err := ports.Parse()
				Expect(err).To(HaveOccurred())
			})

			It("errors on non-numeric input", func() {
				ports = "adsfasdf"
				_, err := ports.Parse()
				Expect(err).To(HaveOccurred())
			})

			It("errors when range starts with lower number", func() {
				ports = "10000-9999"
				_, err := ports.Parse()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("range (10000-9999) must be in ascending numeric order"))
			})
		})
	})

	Describe("Range", func() {
		Describe("Overlaps", func() {
			testRange, _ := NewRange(6010, 6020)

			It("validates non-overlapping ranges", func() {
				r, _ := NewRange(6021, 6030)
				Expect(testRange.Overlaps(r)).To(BeFalse())
			})

			It("finds overlapping ranges of single values", func() {
				r1, _ := NewRange(6010, 6010)
				r2, _ := NewRange(6010, 6010)
				Expect(r1.Overlaps(r2)).To(BeTrue())
			})

			It("finds overlapping ranges of single value and range", func() {
				r2, _ := NewRange(6015, 6015)
				Expect(testRange.Overlaps(r2)).To(BeTrue())
			})

			It("finds overlapping ranges of single value upper bound and range", func() {
				r2, _ := NewRange(6020, 6020)
				Expect(testRange.Overlaps(r2)).To(BeTrue())
			})

			It("validates single value one above upper bound range", func() {
				r2, _ := NewRange(6021, 6021)
				Expect(testRange.Overlaps(r2)).To(BeFalse())
			})

			It("finds overlapping ranges when start overlaps", func() {
				r, _ := NewRange(6015, 6030)
				Expect(testRange.Overlaps(r)).To(BeTrue())
			})

			It("finds overlapping ranges when end overlaps", func() {
				r, _ := NewRange(6005, 6015)
				Expect(testRange.Overlaps(r)).To(BeTrue())
			})

			It("finds overlapping ranges when the range is a superset", func() {
				r, _ := NewRange(6009, 6021)
				Expect(testRange.Overlaps(r)).To(BeTrue())
			})
		})
	})

	Describe("Route", func() {
		var (
			route      Route
			otherRoute Route
			matches    bool
		)

		BeforeEach(func() {
			tag, err := NewModificationTag()
			Expect(err).ToNot(HaveOccurred())
			route = Route{
				Route:           "/foo/bar",
				Port:            35,
				IP:              "2.2.2.2",
				TTL:             66,
				LogGuid:         "banana",
				ModificationTag: tag,
			}
		})

		JustBeforeEach(func() {
			matches = route.Matches(otherRoute)
		})

		Context("Matches", func() {
			Context("when all properties matches", func() {
				BeforeEach(func() {
					otherRoute = route
				})

				It("returns true", func() {
					Expect(matches).To(BeTrue())
				})
			})

			Context("when all properties but modification tag matches", func() {
				BeforeEach(func() {
					otherRoute = route
					tag1, err := NewModificationTag()
					Expect(err).ToNot(HaveOccurred())
					otherRoute.ModificationTag = tag1
				})

				It("returns true", func() {
					Expect(matches).To(BeTrue())
				})
			})
			Context("when some properties don't match", func() {

				BeforeEach(func() {
					otherRoute = Route{
						Route:   "/foo/brah",
						Port:    35,
						IP:      "3.3.3.3",
						LogGuid: "banana",
					}
				})

				It("returns false", func() {
					Expect(matches).To(BeFalse())
				})

			})
		})

	})

	Describe("TcpRouteMapping", func() {
		var (
			route      TcpRouteMapping
			otherRoute TcpRouteMapping
			matches    bool
		)

		BeforeEach(func() {
			tag, err := NewModificationTag()
			Expect(err).ToNot(HaveOccurred())
			route = TcpRouteMapping{
				TcpRoute: TcpRoute{
					RouterGroupGuid: "router-group-1",
					ExternalPort:    60000,
				},
				HostIP:          "2.2.2.2",
				HostPort:        64000,
				TTL:             66,
				ModificationTag: tag,
			}
		})

		JustBeforeEach(func() {
			matches = route.Matches(otherRoute)
		})

		Context("Matches", func() {
			Context("when all properties matches", func() {
				BeforeEach(func() {
					otherRoute = route
				})

				It("returns true", func() {
					Expect(matches).To(BeTrue())
				})
			})

			Context("when all properties but modification tag matches", func() {
				BeforeEach(func() {
					otherRoute = route
					tag1, err := NewModificationTag()
					Expect(err).ToNot(HaveOccurred())
					otherRoute.ModificationTag = tag1
				})

				It("returns true", func() {
					Expect(matches).To(BeTrue())
				})
			})

			Context("when some properties don't match", func() {

				BeforeEach(func() {
					otherRoute = TcpRouteMapping{
						TcpRoute: TcpRoute{
							RouterGroupGuid: "router-group-1",
							ExternalPort:    60000,
						},
						HostIP:   "2.2.2.2",
						HostPort: 64000,
						TTL:      67,
					}
				})

				It("returns false", func() {
					Expect(matches).To(BeFalse())
				})

			})
		})
	})

})
