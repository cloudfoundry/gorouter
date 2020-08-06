package integration

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("KeepAlive (HTTP Persistent Connections) to backends", func() {
	var (
		testState *testState

		testAppRoute string
		testApp      *StateTrackingTestApp
	)

	BeforeEach(func() {
		testState = NewTestState()

		testApp = NewUnstartedTestApp(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			_, err := ioutil.ReadAll(r.Body)
			Expect(err).NotTo(HaveOccurred())
			w.WriteHeader(200)
		}))
		testAppRoute = "potato.potato"
	})

	AfterEach(func() {
		if testState != nil {
			testState.StopAndCleanup()
		}
		testApp.Close()
	})

	doRequest := func() {
		assertRequestSucceeds(testState.client,
			testState.newRequest(fmt.Sprintf("http://%s", testAppRoute)))
	}

	Context("when KeepAlives are disabled", func() {
		BeforeEach(func() {
			testState.cfg.DisableKeepAlives = true

			testState.StartGorouterOrFail()
			testApp.Start()
			testState.register(testApp.Server, testAppRoute)
			Expect(testApp.GetConnStates()).To(BeEmpty())
		})

		Specify("connections to backends are closed after each request", func() {
			doRequest()

			By("checking that the connection is closed after the first request")
			connStates := testApp.GetConnStates()
			Expect(connStates).To(HaveLen(3))
			Expect(connStates[0].State).To(Equal("new"))
			Expect(connStates[1].State).To(Equal("active"))
			Expect(connStates[2].State).To(Equal("closed"))

			By("doing a second request")
			doRequest()

			By("checking that the connection is not re-used")
			connStates = testApp.GetConnStates()
			Expect(connStates[0].State).To(Equal("new"))
			Expect(connStates[1].State).To(Equal("active"))
			Expect(connStates[2].State).To(Equal("closed"))
			Expect(connStates[3].State).To(Equal("new"))
			Expect(connStates[4].State).To(Equal("active"))

			By("checking that different connections are used for each request")
			Expect(connStates[0].ConnPtr).NotTo(Equal(connStates[3].ConnPtr))
		})
	})

	Context("when KeepAlives are enabled", func() {
		BeforeEach(func() {
			testState.cfg.DisableKeepAlives = false
			testState.StartGorouterOrFail()
		})

		Context("when connecting to a non-TLS backend", func() {
			BeforeEach(func() {
				testApp.Start()
				testState.register(testApp.Server, testAppRoute)
			})

			Specify("connections to backends are persisted after requests finish", func() {
				doRequest()
				assertConnectionIsReused(testApp.GetConnStates(), "new", "active", "idle")

				doRequest()
				assertConnectionIsReused(testApp.GetConnStates(), "new", "active", "idle", "active", "idle")

				By("re-registering the route")
				testState.register(testApp.Server, testAppRoute)

				By("doing a third request")
				doRequest()

				By("checking that the same connection is *still* being re-used on the backend")
				assertConnectionIsReused(testApp.GetConnStates()[:6],
					"new", "active", "idle", "active", "idle", "active")
			})
		})

		Context("when connecting to a TLS-enabled backend", func() {
			BeforeEach(func() {
				testApp.TLS = testState.trustedBackendTLSConfig
				testApp.StartTLS()
				testState.registerAsTLS(testApp.Server, testAppRoute, testState.trustedBackendServerCertSAN)
			})

			Specify("connections to backends are persisted after requests finish", func() {
				By("doing a couple requests")
				doRequest()
				doRequest()

				By("checking that only one backend connection is used for both requests")
				assertConnectionIsReused(testApp.GetConnStates()[:4], "new", "active", "idle", "active")

				By("re-registering the route")
				testState.registerAsTLS(testApp.Server, testAppRoute, testState.trustedBackendServerCertSAN)

				By("doing a third request")
				doRequest()

				By("checking that the same connection is *still* being re-used on the backend")
				// We don't need to assert on the last "idle" since we know the connection is being reused by not seeing a "new"
				assertConnectionIsReused(testApp.GetConnStates()[:6],
					"new", "active", "idle", "active", "idle", "active")
			})
		})
	})
})

type ConnState struct {
	ConnPtr    string
	RemoteAddr string
	State      string
}

type StateTrackingTestApp struct {
	*httptest.Server
	backendConnStates []ConnState
	m                 sync.Mutex
}

func (s *StateTrackingTestApp) GetConnStates() []ConnState {
	s.m.Lock()
	defer s.m.Unlock()
	ret := make([]ConnState, len(s.backendConnStates))
	copy(ret, s.backendConnStates) // copy(dst, src)
	return ret
}

func assertConnectionIsReused(actualStates []ConnState, expectedStates ...string) {
	// get initial connection
	p := actualStates[0].ConnPtr
	a := actualStates[0].RemoteAddr

	// check length
	Expect(actualStates).To(HaveLen(len(expectedStates)))

	// construct slice of expected connection state values
	expectedConnStates := make([]ConnState, len(expectedStates))
	for i := 0; i < len(expectedStates); i++ {
		expectedConnStates[i] = ConnState{ConnPtr: p, RemoteAddr: a, State: expectedStates[i]}
	}

	// assert
	Expect(actualStates).To(Equal(expectedConnStates))
}

func NewUnstartedTestApp(handler http.Handler) *StateTrackingTestApp {
	a := &StateTrackingTestApp{
		Server: httptest.NewUnstartedServer(handler),
	}
	a.Server.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		a.m.Lock()
		defer a.m.Unlock()
		a.backendConnStates = append(a.backendConnStates,
			ConnState{
				ConnPtr:    fmt.Sprintf("%p", conn),
				RemoteAddr: conn.RemoteAddr().String(),
				State:      state.String(),
			})
	}
	a.Server.Config.IdleTimeout = 5 * time.Second
	return a
}
