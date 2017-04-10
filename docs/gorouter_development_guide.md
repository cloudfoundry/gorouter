# Development Guide for Gorouter

Recommended reading before diving into Gorouter code:
- [Hypertext Transfer Protocol](https://en.wikipedia.org/wiki/Hypertext_Transfer_Protocol#Message_format)
- [How to use interfaces in Go](http://jordanorelli.com/post/32665860244/how-to-use-interfaces-in-go)
- [Golang Concurrency](http://www.golangbootcamp.com/book/concurrency)
- [http.Transport.RoundTrip](https://golang.org/pkg/net/http/#Transport.RoundTrip)
- [http.RoundTripper](https://golang.org/pkg/net/http/#RoundTripper)
- [http.ResponseWriter](https://golang.org/pkg/net/http/#ResponseWriter)
- [http.Request](https://golang.org/pkg/net/http/#Request)

- [Gorouter README.md](https://github.com/cloudfoundry/gorouter#gorouter)

## Golang TCP Networking Basics
Nearly all of the networking logic in Golang is dealt with the same pattern
as if one were dealing with a raw TCP connection.

Just as a general overview of how TCP networking works in Golang, let's go
through a sample set of applications that read and write from/to a TCP
connection.

Establishing a TCP connection requires a `Dial` from the client side and a
`Listen` on the server side. Since `Listen` can accept multiple simultaneous
connections, it must call `Accept` for every connection it handles.

Once you receive a `net.Conn` object to work with, there are three basic
methods on the `net.Conn` interface: `Read`, `Write`, and `Close`.

`Close` is self explanatory. `Read` and `Write` are blocking network calls that
block until some amount of data is read/written. They return error `io.EOF` when
the connection is closed. This is the only way to know whether or not a
connection has closed. Golang's HTTP package is no exception.

Basic client that subscribes and then prints what it receives:
```go
conn, err := net.Dial("tcp", "192.0.2.1:8080")
if err != nil {
  // handle dial error
}
defer conn.Close()

_, err = conn.Write([]byte("subscribe"))
if err != nil {
  // handle error writing to connection
}

tmpBuf := make([]byte, 1024)
for {
  // conn.Read will block until some amount of data is read, and returns the
  // number of bytes read along with an error. It will return bytes read as well
  // as error `io.EOF` when data is received and the connection is closed, so be
  // sure to process the data before handling `io.EOF`.
  n, readErr := conn.Read(tmpBuf)
  if n > 0 {
    _, err := os.Stdout.Write(tmpBuf[:n])
    if err != nil {
      // handle error printing to standard out
    }
  }
  if readErr == io.EOF {
    // Connection has closed, so quit
    break
  } else {
    // handle non-EOF read err
  }
}
```

Basic server that checks for the subscribe message then sends the client info:
```go
ln, err := net.Listen("tcp", ":8080")
if err != nil {
	// handle error
}
for {
	conn, err := ln.Accept()
	if err != nil {
		// handle error
	}
	go handleConnection(conn)
}
...
func handleConnection(conn net.Conn) {
  defer conn.Close()
  tmpBuf := make([]byte, 16)
  n, readErr := conn.Read(tmpBuf)
  if readErr != nil {
    // handle connection read err / connection close
  }
  if n == 8 && string(tmpBuf[:8]) == "subscribe" {
    for i := 0; i < 5; i++ {
      _, writeErr := conn.Write("hello")
      if writeErr != nil {
        // handle connection write err / connection close
      }
    }
  } else {
    // handle invalid read
  }
}
```

Notice how this example demonstrates something similar to a HTTP `GET` request
and a response with body returned for that request. In fact, this is pretty
much how it's implemented in Golang's `net/http`, except it has a lot more
logic to follow the protocol.

Next time you use a http.ResponseWriter, think of it as a very thin wrapper
on top of `conn.Write` that only handles writing the HTTP headers for you.

## General Gorouter Architecture

Here is a general dependency graph (X-->Y means X is dependent on Y) for
the components of Gorouter.

![architecture](https://cdn.rawgit.com/flawedmatrix/gorouter-architecture-docs/master/architecture.svg)

We'll go over some of these components later in this document, but this should
serve as a good starting point to where to start looking for the important
components of Gorouter.

`main.go` is also a good place to start looking to see how everything is
initialized. Notice that `nats-subscriber` and `route_fetcher` are initialized
in `main`, but they are depended on by the route registry.

## Ifrit processes
Here is the anatomy of a Ifrit process:

![ifrit](https://cdn.rawgit.com/flawedmatrix/gorouter-architecture-docs/master/ifrit.svg)

Our Ifrit processes are used for long running routines inside Gorouter,
e.g. serving HTTP requests with the router, or periodically fetching routes
from Routing API. There exist a few long running processes in Gorouter that
aren't fully implemented with the Ifrit workflow. e.g. NATS subscriptions
(mbus package), and the route registry pruning cycle (registry package).

## What does Gorouter do?
It basically forwards requests from the client to backend instances of an app.

Here is a very basic depiction of what Gorouter does:
![basic request](https://cdn.rawgit.com/flawedmatrix/gorouter-architecture-docs/master/basic_request.svg)

Route services are a bit tricky, but they involve two separate requests to
the route for the backend app through the Gorouter:
![route service request](https://cdn.rawgit.com/flawedmatrix/gorouter-architecture-docs/master/routeservice.svg)

Here's a more detailed inspection of the request-response flow through
the Gorouter:
![indepth request](https://cdn.rawgit.com/flawedmatrix/gorouter-architecture-docs/master/indepth_request.svg)

## What are all these extra components in the Gorouter request flow?
Most of the request processing logic lives in the [negroni
handlers](https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/proxy/proxy.go#l107-l120).
Note that it usually isn't possible to implement any Response modification logic
in these handlers! That logic is mostly handled by the `ProxyRoundTripper`

Nearly all of the important logic is implemented as part of a
`ServeHTTP(http.ResponseWriter,*http.Request)` function.

### Negroni Handlers
1. `ProxyResponseWriter` augments the `ResponseWriter` with helpers and records
  response body length
  - https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/handlers/proxywriter.go
  - https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/proxy/utils/responsewriter.go
1. [sets the `X-Vcap-Request-Id` header](https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/handlers/request_id.go)
1. [records the request and response in the `access.log` file](https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/handlers/access_log.go)
1. [reports response code and latency for metrics](https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/handlers/reporter.go)
1. [responds to healthcheck requests](https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/handlers/proxy_healthcheck.go)
1. [handles Zipkin headers](https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/handlers/zipkin.go)
1. [checks HTTP protocol version](https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/handlers/protocolcheck.go)
1. [**looks up backends for requested route**](https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/handlers/lookup.go)
1. [determines whether or not the request should go to a route service](https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/handlers/routeservice.go)
1. [handles TCP or WebSocket upgrade](https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/proxy/proxy.go#L167-L210)
1. [**httputil.ReverseProxy** transforms the request into a request to the next hop](https://golang.org/src/net/http/httputil/reverseproxy.go?h=ReverseProxy#L28)

### ProxyRoundTripper
https://github.com/cloudfoundry/gorouter/blob/3ceca97b71afe458679d2893f1c6b3c04f5b995a/proxy/round_tripper/proxy_round_tripper.go

This component executes the request to the next hop (whether it be to a backend or to a route service).

Its responsibilities are:
  1. Forwarding the request to either a backend or a route service (via the
    `RoundTrip` method).
  1. Retry failed requests.
  1. Select the next endpoint in a set of backends for the requested route.
    There are currently two different strategies for selecting the next
    endpoint:: choose them in a Round Robin fashion, or choose the endpoint
    with the least connections.
  1. Setting trace headers on the response.
  1. Setting sticky session cookies on the response. Sticky sessions are cookies
    that allow clients to make requests to the same instance of the backend app.
