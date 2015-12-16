[![Build Status](https://travis-ci.org/cloudfoundry/gorouter.svg?branch=master)](https://travis-ci.org/cloudfoundry/gorouter)

# GoRouter

This repository contains the source code for a Go implementation of the Cloud
Foundry router.

You can find the old router [here](http://github.com/cloudfoundry-attic/router)

## Getting started

The following instructions may help you get started with gorouter in a
standalone environment.

### External Dependencies

- Go should be installed and in the PATH
- GOPATH should be set as described in http://golang.org/doc/code.html
- [gnatsd](https://github.com/apcera/gnatsd) installed and in the PATH
- [godep](https://github.com/tools/godep) installed and in the PATH
- Install [direnv](http://direnv.net/) if you are planning to do gorouter
development as part of cf-release.

### Development Setup

Download gorouter:

Option 1: GoRouter (standalone)
```bash
go get -d github.com/cloudfoundry/gorouter
cd $GOPATH/src/github.com/cloudfoundry/gorouter
```

Option 2: GoRouter (as part of [cf-release](https://github.com/cloudfoundry/cf-release))
```bash
git clone https://github.com/cloudfoundry/cf-release
cd cf-release
./update
cd cf-release/src/github.com/cloudfoundry/gorouter
```
 *Note: direnv will automatically set your GOPATH when you cd into the gorouter directory. You will need to run `direnv allow` the first time.*


To install exactly the dependencies vendored with gorouter, use [godep](https://github.com/tools/godep):

```bash
go get -v github.com/tools/godep
godep restore ./...
```




### Running Tests

We are using [Ginkgo](https://github.com/onsi/ginkgo), to run tests.

Running `scripts/test` will:
- Check for Go
- Check that GOPATH is set
- Download & Install gnatsd (or use the one already downloaded into the GOPATH)
- Update the PATH to prepend the godep workspace
- Install ginkgo (from the godep vendored sources into the godep workspace bin)
- Run all the tests with ginkgo (in random order, without benchmarks, using the vendored godep dependencies)

Any flags passed into `scripts/test` will be passed into ginkgo.

```bash
# run all the tests
scripts/test

# run only tests whose names match Registry
scripts/test -focus=Registry

# run only the tests in the registry package
scripts/test registry
```

To run the tests using GOPATH dependency sources (bypassing vendored dependencies):

```bash
ginkgo -r
```

### Building
Building creates an executable in the gorouter/ dir:

```bash
go build
```

### Installing
Installing creates an executable in the $GOPATH/bin dir:

```bash
go install
```

### Start

```bash
# Start NATS server in daemon mode
go get github.com/apcera/gnatsd
gnatsd &

# Start gorouter
gorouter
```

### Usage

When the gorouter starts, it sends a `router.start` message.
This message contains an interval that other components should then send `router.register` on, `minimumRegisterIntervalInSeconds`.
It is recommended that clients should send `router.register` messages on this interval.
This `minimumRegisterIntervalInSeconds` value is configured through the `start_response_delay_interval` configuration value.
The gorouter will prune routes that it considers to be stale based upon a seperate "staleness" value, `droplet_stale_threshold`, which defaults to 120 seconds.
The gorouter will check if routes have become stale on an interval defined by `prune_stale_droplets_interval`, which defaults to 30 seconds.
All of these values are represented in seconds and will always be integers.

The format of the `router.start` message is as follows:

```json
{
  "id": "some-router-id",
  "hosts": ["1.2.3.4"],
  "minimumRegisterIntervalInSeconds": 5,
  "prunteThresholdInSeconds": 120,
}
```
After a `router.start` message is received by a client, the client should send `router.register` messages. This ensures that the new router can update its routing table and synchronize with existing routers.

If a component comes online after the router, it must make a NATS request
called `router.greet` in order to determine the interval. The response to this
message will be the same format as `router.start`.

The format of the `router.register` message is as follows:

```json
{
  "host": "127.0.0.1",
  "port": 4567,
  "uris": [
    "my_first_url.vcap.me",
    "my_second_url.vcap.me"
  ],
  "tags": {
    "another_key": "another_value",
    "some_key": "some_value"
  },
  "app": "some_app_guid",
  "stale_threshold_in_seconds": 120,
  "private_instance_id": "some_app_instance_id"
}
```
`stale_threshold_in_seconds` is the custom staleness threshold for the route being registered. If this value is not sent, it will default to the router's default staleness threshold.
`app` is a unique identifier for an application that the route is registered for. It is used to emit router access logs associated with the app through dropsonde.
`private_instance_id` is a unique identifier for an instance associated with the app identified by the `app` field. `X-CF-InstanceID` is set to this value on the request to the endpoint registered.

Such a message can be sent to both the `router.register` subject to register
URIs, and to the `router.unregister` subject to unregister URIs, respectively.

###Example

Create a simple app
```
$ nohup ruby -rsinatra -e 'get("/") { "Hello!" }' &
```

Send a register message
```
$ nats-pub 'router.register' '{"host":"127.0.0.1","port":4567,"uris":["my_first_url.vcap.me","my_second_url.vcap.me"],"tags":{"another_key":"another_value","some_key":"some_value"}}'

Published [router.register] : '{"host":"127.0.0.1","port":4567,"uris":["my_first_url.vcap.me","my_second_url.vcap.me"],"tags":{"another_key":"another_value","some_key":"some_value"}}'
```

See that it works!
```
$ curl my_first_url.vcap.me:8081
Hello!
```

### Instrumentation

Gorouter provides a `/varz` http endpoint for monitoring.

There is a *deprecated* `healthz` endpoint that provides no useful information about the router. To check on the health of the router, we currently recommend checking the status of TCP port 80.

The `/routes` endpoint returns the entire routing table as JSON. Each route has an associated array of host:port entries.

Aside from the two monitoring http endpoints (which are only reachable via the status port), specifying the `User-Agent` header with a value of `HTTP-Monitor/1.1` also returns the current health of the router. This is particularly useful when performing healthchecks from a Load Balancer.

Because of the nature of the data present in `/varz` and `/routes`, they require http basic authentication credentials which can be acquired through NATS. The `port`, `user` and password (`pass` is the config attribute) can be explicitly set in the gorouter.yml config file's `status` section.

```
status:
  port: 8080
  user: some_user
  pass: some_password
```

Example interaction with curl:

```
curl -vvv -A "HTTP-Monitor/1.1" http://127.0.0.1/
* About to connect() to 127.0.0.1 port 80 (#0)
*   Trying 127.0.0.1... connected
> GET / HTTP/1.1
> User-Agent: HTTP-Monitor/1.1
> Host: 127.0.0.1
> Accept: */*
>
< HTTP/1.1 200 OK
< Cache-Control: private, max-age=0
< Expires: 0
< Date: Mon, 10 Feb 2014 00:55:25 GMT
< Transfer-Encoding: chunked
<
ok
* Connection #0 to host 127.0.0.1 left intact
* Closing connection #0

curl -vvv "http://someuser:somepass@127.0.0.1:8080/routes"
* About to connect() to 127.0.0.1 port 8080 (#0)
*   Trying 127.0.0.1...
* connected
* Connected to 127.0.0.1 (127.0.0.1) port 8080 (#0)
* Server auth using Basic with user 'someuser'
> GET /routes HTTP/1.1
> Authorization: Basic c29tZXVzZXI6c29tZXBhc3M=
> User-Agent: curl/7.24.0 (x86_64-apple-darwin12.0) libcurl/7.24.0 OpenSSL/0.9.8r zlib/1.2.5
> Host: 127.0.0.1:8080
> Accept: */*
>
< HTTP/1.1 200 OK
< Content-Type: application/json
< Date: Mon, 25 Mar 2013 20:31:27 GMT
< Transfer-Encoding: chunked
<
{"0295dd314aaf582f201e655cbd74ade5.cloudfoundry.me":["127.0.0.1:34567"],"03e316d6aa375d1dc1153700da5f1798.cloudfoundry.me":["127.0.0.1:34568"]}
```

### Profiling the Server

The GoRouter runs the [cf_debug_server](https://github.com/cloudfoundry-incubator/cf-debug-server), which is a wrapper around the go pprof tool. In order to generate this profile, do the following:

```bash
# Establish a SSH tunnel to your server (not necessary if you can connect directly)
ssh -L localhost:8080:[INTERNAL_SERVER_IP]:17001 vcap@[BOSH_DIRECTOR]
# Run the profile tool.
go tool pprof http://localhost:8080/debug/pprof/profile
```

## Load Balancing

The GoRouter is, in simple terms, a reverse proxy that load balances between many backend instances. The implementation currently uses simple round-robin load balancing and will retry a request if the chosen backend does not accept the TCP connection.

## Logs

The router's logging is specified in its YAML configuration file, in a [steno configuration format](http://github.com/cloudfoundry/steno#from-yaml-file).
The meanings of the router's log levels are as follows:

* `fatal` - An error has occurred that makes the current request unservicable.
Examples: the router can't bind to its TCP port, a CF component has published invalid data to the router.
* `warn` - An unexpected state has occurred. Examples: the router tried to publish data that could not be encoded as JSON
* `info`, `debug` - An expected event has occurred. Examples: a new CF component was registered with the router, the router has begun
to prune routes for stale droplets.

Access logs provide information for the following fields when recieving a request:

`<Request Host> - [<Start Date>] "<Request Method> <Request URL> <Request Protocol>" <Status Code> <Bytes Received> <Bytes Sent> "<Referer>" "<User-Agent>" <Remote Address> x_forwarded_for:"<X-Forwarded-For>" x_forwarded_proto:"<X-Forwarded-Proto>" vcap_request_id:<X-Vcap-Request-ID> response_time:<Response Time> app_id:<Application ID> <Extra Headers>`
* Status Code, Response Time, Application ID, and Extra Headers are all optional fields
* The absence of Status Code, Response Time or Application ID will result in a "-" in the corresponding field

## Contributing

Please read the [contributors' guide](https://github.com/cloudfoundry/gorouter/blob/master/CONTRIBUTING.md)
