package token_fetcher_test

import (
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	. "github.com/cloudfoundry-incubator/uaa-token-fetcher"

	"bytes"

	trace "github.com/cloudfoundry-incubator/trace-logger"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

var verifyBody = func(expectedBody string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		Expect(err).ToNot(HaveOccurred())

		defer r.Body.Close()
		Expect(string(body)).To(Equal(expectedBody))
	}
}

var _ = Describe("TokenFetcher", func() {
	var (
		cfg    *OAuthConfig
		server *ghttp.Server
	)

	BeforeEach(func() {
		cfg = &OAuthConfig{}
		server = ghttp.NewServer()

		url, err := url.Parse(server.URL())
		Expect(err).ToNot(HaveOccurred())

		addr := strings.Split(url.Host, ":")

		cfg.TokenEndpoint = "http://" + addr[0]
		cfg.Port, err = strconv.Atoi(addr[1])
		Expect(err).ToNot(HaveOccurred())

		cfg.ClientName = "client-name"
		cfg.ClientSecret = "client-secret"
	})

	AfterEach(func() {
		server.Close()
	})

	Describe(".NewTokenFetcher", func() {
		It("maybe does something interesting", func() {})
	})

	Describe(".FetchToken", func() {
		Context("when OAuth server cannot be reached", func() {
			It("returns an error", func() {
				cfg.TokenEndpoint = "http://bogus.url"
				fetcher := NewTokenFetcher(cfg)
				_, err := fetcher.FetchToken()
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when the respose body is malformed", func() {
			It("returns an error", func() {
				server.AppendHandlers(
					ghttp.RespondWithJSONEncoded(http.StatusOK, "broken garbage response"),
				)

				fetcher := NewTokenFetcher(cfg)
				_, err := fetcher.FetchToken()
				Expect(err).To(HaveOccurred())
				Expect(server.ReceivedRequests()).Should(HaveLen(1))
			})
		})

		Context("when a non 200 OK is returned", func() {
			It("returns an error", func() {
				server.AppendHandlers(
					ghttp.RespondWith(http.StatusBadRequest, "you messed up"),
				)

				fetcher := NewTokenFetcher(cfg)
				_, err := fetcher.FetchToken()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("status code: 400, body: you messed up"))
				Expect(server.ReceivedRequests()).Should(HaveLen(1))
			})
		})

		It("returns a new token", func() {
			responseBody := &Token{
				AccessToken: "the token",
				ExpireTime:  20,
			}

			server.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", "/oauth/token"),
					ghttp.VerifyBasicAuth("client-name", "client-secret"),
					ghttp.VerifyContentType("application/x-www-form-urlencoded; charset=UTF-8"),
					ghttp.VerifyHeader(http.Header{
						"Accept": []string{"application/json; charset=utf-8"},
					}),
					verifyBody("grant_type=client_credentials"),
					ghttp.RespondWithJSONEncoded(http.StatusOK, responseBody),
				))

			fetcher := NewTokenFetcher(cfg)
			token, err := fetcher.FetchToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(server.ReceivedRequests()).Should(HaveLen(1))
			Expect(token.AccessToken).To(Equal("the token"))
			Expect(token.ExpireTime).To(Equal(20))
		})

		It("logs requests and responses", func() {
			stdout := bytes.NewBuffer([]byte{})
			trace.SetStdout(stdout)
			trace.NewLogger("true")

			server.AppendHandlers(
				ghttp.RespondWith(http.StatusBadRequest, "you messed up"),
			)

			fetcher := NewTokenFetcher(cfg)
			_, err := fetcher.FetchToken()
			Expect(err).To(HaveOccurred())

			r, err := ioutil.ReadAll(stdout)
			log := string(r)
			Expect(err).NotTo(HaveOccurred())
			Expect(log).To(ContainSubstring("REQUEST: "))
			Expect(log).To(ContainSubstring("POST /oauth/token HTTP/1.1"))

			Expect(log).To(ContainSubstring("RESPONSE: "))
			Expect(log).To(ContainSubstring("HTTP/1.1 400 Bad Request"))
		})
	})
})
