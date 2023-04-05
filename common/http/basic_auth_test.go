package http_test

import (
	. "code.cloudfoundry.org/gorouter/common/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"net"
	"net/http"
)

var _ = Describe("http", func() {
	var listener net.Listener

	AfterEach(func() {
		if listener != nil {
			listener.Close()
		}
	})

	bootstrap := func(x Authenticator) *http.Request {
		var err error

		h := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}

		y := &BasicAuth{Handler: http.HandlerFunc(h), Authenticator: x}

		z := &http.Server{Handler: y}

		l, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())

		go z.Serve(l)

		// Keep listener around such that test teardown can close it
		listener = l

		r, err := http.NewRequest("GET", "http://"+l.Addr().String(), nil)
		Expect(err).ToNot(HaveOccurred())
		return r
	}

	Context("Unauthorized", func() {
		It("without credentials", func() {
			req := bootstrap(nil)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())

			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("with invalid header", func() {
			req := bootstrap(nil)

			req.Header.Set("Authorization", "invalid")

			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("with bad credentials", func() {
			f := func(u, p string) bool {
				Expect(u).To(Equal("user"))
				Expect(p).To(Equal("bad"))
				return false
			}

			req := bootstrap(f)

			req.SetBasicAuth("user", "bad")

			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})
	})
	It("succeeds with good credentials", func() {
		f := func(u, p string) bool {
			Expect(u).To(Equal("user"))
			Expect(p).To(Equal("good"))
			return true
		}

		req := bootstrap(f)

		req.SetBasicAuth("user", "good")

		resp, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})
