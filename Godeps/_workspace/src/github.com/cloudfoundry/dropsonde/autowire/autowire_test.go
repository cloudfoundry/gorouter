package autowire_test

import (
	"github.com/cloudfoundry/dropsonde/autowire"
	"github.com/cloudfoundry/dropsonde/emitter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"net/http"
	"os"
	"reflect"
)

var _ = Describe("Autowire", func() {
	var oldDestination string
	var oldOrigin string

	BeforeEach(func() {
		oldDestination = os.Getenv("DROPSONDE_DESTINATION")
		oldOrigin = os.Getenv("DROPSONDE_ORIGIN")
	})

	AfterEach(func() {
		os.Setenv("DROPSONDE_DESTINATION", oldDestination)
		os.Setenv("DROPSONDE_ORIGIN", oldOrigin)
	})

	Describe("Initialize", func() {
		Context("with a non-nil emitter", func() {
			It("instruments the HTTP default transport", func() {
				autowire.Initialize(emitter.NewEventEmitter(nil, ""))
				Expect(reflect.TypeOf(http.DefaultTransport).Elem().Name()).ToNot(Equal("Transport"))
			})
		})

		Context("with a nil-emitter", func() {
			It("resets the HTTP default transport to not be instrumented", func() {
				autowire.Initialize(nil)
				Expect(reflect.TypeOf(http.DefaultTransport).Elem().Name()).To(Equal("Transport"))
			})
		})
	})

	Describe("CreateDefaultEmitter", func() {
		Context("with DROPSONDE_ORIGIN set", func() {
			BeforeEach(func() {
				os.Setenv("DROPSONDE_ORIGIN", "anything")
			})

			Context("with DROPSONDE_DESTINATION missing", func() {
				It("defaults to localhost", func() {
					os.Setenv("DROPSONDE_DESTINATION", "")
					_, destination := autowire.CreateDefaultEmitter()

					Expect(destination).To(Equal("localhost:3457"))
				})
			})

			Context("with DROPSONDE_DESTINATION set", func() {
				It("uses the configured destination", func() {
					os.Setenv("DROPSONDE_DESTINATION", "test")
					_, destination := autowire.CreateDefaultEmitter()

					Expect(destination).To(Equal("test"))
				})
			})
		})

		Context("with DROPSONDE_ORIGIN missing", func() {
			It("returns a nil-emitter", func() {
				os.Setenv("DROPSONDE_ORIGIN", "")
				emitter, _ := autowire.CreateDefaultEmitter()
				Expect(emitter).To(BeNil())
			})
		})
	})
})

type FakeHandler struct{}

func (fh FakeHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {}

type FakeRoundTripper struct{}

func (frt FakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, nil
}
