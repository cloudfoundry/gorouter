[![Build Status](https://travis-ci.org/cloudfoundry/gorouter.svg?branch=master)](https://travis-ci.org/cloudfoundry/gorouter) [![Go Report Card](https://goreportcard.com/badge/github.com/cloudfoundry/gorouter)](https://goreportcard.com/report/github.com/cloudfoundry/gorouter)

# GoRouter
This repository contains the source code for the Cloud Foundry L7 HTTP router. GoRouter is deployed by default with Cloud Foundry ([cf-release](https://github.com/cloudfoundry/cf-release)) which includes [routing-release](https://github.com/cloudfoundry-incubator/routing-release) as submodule.

**Note**: This repository should be imported as `code.cloudfoundry.org/gorouter`.

## Development

The following instructions may help you get started with gorouter.

### Prerequisites

- Go should be installed and in the PATH
- GOPATH should be set as described in http://golang.org/doc/code.html
- [gnatsd](https://github.com/nats-io/gnatsd) installed and in the PATH
- Install [direnv](http://direnv.net/)

### Setup

GoRouter dependencies are managed with [routing-release](https://github.com/cloudfoundry-incubator/routing-release#). Do not clone the gorouter repo directly; instead, follow instructions at https://github.com/cloudfoundry-incubator/routing-release#get-the-code (summarized below).

```bash
git clone https://github.com/cloudfoundry-incubator/routing-release
cd routing-release
./scripts/update
cd src/code.cloudfoundry.org/gorouter
```
 *Note: direnv will automatically set your GOPATH when you cd into the routing-release directory. You will need to run `direnv allow` the first time.*

### Running Tests

We are using [Ginkgo](https://github.com/onsi/ginkgo), to run tests.

Running `scripts/test` will:
- Checks for Go
- Checks that GOPATH is set
- Installs gnatsd and ginkgo (or use the one already downloaded into the GOPATH)
- Runs all the tests with ginkgo (in random order, without benchmarks)

Any flags passed into `scripts/test` will be passed into ginkgo.

```bash
# run all the tests
scripts/test

# run only tests whose names match Registry
scripts/test -focus=Registry

# run only the tests in the registry package
scripts/test registry
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
go get github.com/nats-io/gnatsd
gnatsd &

# Start gorouter
gorouter
```

## Performance

See [Routing Release 0.144.0 Release Notes](https://github.com/cloudfoundry-incubator/routing-release/releases/tag/0.144.0)

## Dynamic Routing Table

Gorouters routing table is updated dynamically via the NATS message bus. NATS can be deployed via BOSH with ([cf-release](https://github.com/cloudfoundry/cf-release)) or standalone using [nats-release](https://github.com/cloudfoundry/nats-release).

To add or remove a record from the routing table, a NATS client must send register or unregister messages. Records in the routing table have a maximum TTL of 120 seconds, so clients must heartbeat registration messages periodically; we recommend every 20s. [Route Registrar](https://github.com/cloudfoundry/route-registrar) is a BOSH job that comes with [Routing Release](https://github.com/cloudfoundry-incubator/routing-release) that automates this process.

When deployed with Cloud Foundry, registration of routes for apps pushed to CF occurs automatically without user involvement. For details, see [Routes and Domains](https://docs.cloudfoundry.org/devguide/deploy-apps/routes-domains.html).

### Registering Routes via NATS

When the gorouter starts, it sends a `router.start` message to NATS. This message contains an interval that other components should then send `router.register` on, `minimumRegisterIntervalInSeconds`. It is recommended that clients should send `router.register` messages on this interval. This `minimumRegisterIntervalInSeconds` value is configured through the `start_response_delay_interval` configuration property. GoRouter will prune routes that it considers to be stale based upon a seperate "staleness" value, `droplet_stale_threshold`, which defaults to 120 seconds. GoRouter will check if routes have become stale on an interval defined by `prune_stale_droplets_interval`, which defaults to 30 seconds. All of these values are represented in seconds and will always be integers.

The format of the `router.start` message is as follows:

```json
{
  "id": "some-router-id",
  "hosts": ["1.2.3.4"],
  "minimumRegisterIntervalInSeconds": 20,
  "prunteThresholdInSeconds": 120,
}
```

After a `router.start` message is received by a client, the client should send `router.register` messages. This ensures that the new router can update its routing table and synchronize with existing routers.

If a component comes online after the router, it must make a NATS request called `router.greet` in order to determine the interval. The response to this message will be the same format as `router.start`.

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
  "private_instance_id": "some_app_instance_id",
  "isolation_segment": "some_iso_seg_name"
}
```

`stale_threshold_in_seconds` is the custom staleness threshold for the route being registered. If this value is not sent, it will default to the router's default staleness threshold.

`app` is a unique identifier for an application that the endpoint is registered for. This value will be included in router access logs with the label `app_id`, as well as being sent with requests to the endpoint in an HTTP header `X-CF-ApplicationId`.

`private_instance_id` is a unique identifier for an instance associated with the app identified by the `app` field. Gorouter includes an HTTP header `X-CF-InstanceId` set to this value with requests to the registered endpoint.

`isolation_segment` determines which routers will register route. Only Gorouters configured with the matching isolation segment will register the route. If a value is not provided, the route will be registered only by Gorouters set to the `all` or `shared-and-segments` router table sharding modes. Refer to the job properties for [Gorouter](https://github.com/cloudfoundry-incubator/routing-release/blob/develop/jobs/gorouter/spec) for more information.

Such a message can be sent to both the `router.register` subject to register
URIs, and to the `router.unregister` subject to unregister URIs, respectively.

### Example

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

**Note:** In order to use `nats-pub` to register a route, you must run the command on the NATS VM. If you are using [`cf-deployment`](https://github.com/cloudfoundry/cf-deployment), you can run `nats-pub` from any VM.  

## Healthchecking from a Load Balancer

To scale GoRouter horizontally for high-availability or throughput capacity, you
must deploy it behind a highly-available load balancer (F5, AWS ELB, etc).

GoRouter has a health endpoint `/health` on port 8080 that returns a 200 OK which indicates
the GoRouter instance is healthy; any other response indicates unhealthy.
This port can be configured via the `router.status.port` property in the BOSH
deployment manifest or via the `status.port` property under
`/var/vcap/jobs/gorouter/config/gorouter.yml`


```
$ curl -v http://10.0.32.15:8080/health
*   Trying 10.0.32.15..
* Connected to 10.0.32.15 (10.0.32.15) port 8080 (#0)
> GET /health HTTP/1.1
> Host: 10.0.32.15:8080
> User-Agent: curl/7.43.0
> Accept: */*
>
< HTTP/1.1 200 OK
< Cache-Control: private, max-age=0
< Expires: 0
< Date: Thu, 22 Sep 2016 00:13:54 GMT
< Content-Length: 3
< Content-Type: text/plain; charset=utf-8
<
ok
* Connection #0 to host 10.0.32.15 left intact
```

**DEPRECATED:**
Your load balancer can be configured to send an HTTP healthcheck on
port 80 with the `User-Agent` HTTP header set to `HTTP-Monitor/1.1`. A 200
response indicates the GoRouter instance is healthy; any other response
indicates unhealthy. GoRouter can be configured to accept alternate values for
the User Agent header using the `healthcheck_user_agent` configuration
property; as an example, AWS ELBS send `User-Agent: ELB-HealthChecker/1.0`.

```
$ curl -v -A "HTTP-Monitor/1.1" "http://10.0.32.15"
* Rebuilt URL to: http://10.0.32.15/
* Hostname was NOT found in DNS cache
*   Trying 10.0.32.15...
* Connected to 10.0.32.15 (10.0.32.15) port 80 (#0)
> GET / HTTP/1.1
> User-Agent: HTTP-Monitor/1.1
> Host: 10.0.32.15
> Accept: */*
>
< HTTP/1.1 200 OK
< Cache-Control: private, max-age=0
< Expires: 0
< X-Vcap-Request-Id: 04ad84c6-43dd-4d20-7818-7c47595d9442
< Date: Thu, 07 Jan 2016 22:30:02 GMT
< Content-Length: 3
< Content-Type: text/plain; charset=utf-8
<
ok
* Connection #0 to host 10.0.32.15 left intact
```

**DEPRECATED:**
The `/healthz` endpoint provides a similar response, but it always returns a 200
response regardless of whether or not the GoRouter instance is healthy.

## Instrumentation

### The Routing Table

The `/routes` endpoint returns the entire routing table as JSON. This endpoint requires basic authentication and is served on port 8080. Each route has an associated array of host:port entries.

```
$ curl "http://someuser:somepass@10.0.32.15:8080/routes"
{"api.catwoman.cf-app.com":[{"address":"10.244.0.138:9022","ttl":0,"tags":{"component":"CloudController"}}],"dora-dora.catwoman.cf-app.com":[{"address":"10.244.16.4:60035","ttl":0,"tags":{"component":"route-emitter"}},{"address":"10.244.16.4:60060","ttl":0,"tags":{"component":"route-emitter"}}]}
```

Because of the nature of the data present in `/varz` and `/routes`, they require http basic authentication credentials. These credentials can be found the BOSH manifest for cf-release under the `router` job:

```
properties:
  router:
    status:
      password: zed292_bevesselled
      port:
      user: paronymy61-polaric
```

If `router.status.user` is not set in the manifest, the default is `router-status` as can be seen from [the job spec](https://github.com/cloudfoundry-incubator/routing-release/blob/develop/jobs/gorouter/spec).

Or on the Gorouter VM under `/var/vcap/jobs/gorouter/config/gorouter.yml`:

```
status:
  port: 8080
  user: some_user
  pass: some_password
```

### Metrics

The `/varz` endpoint provides status and metrics. This endpoint requires basic authentication.

```
$ curl "http://someuser:somepass@10.0.32.15:8080/varz"
{"bad_gateways":0,"bad_requests":20,"cpu":0,"credentials":["user","pass"],"droplets":26,"host":"10.0.32.15:8080","index":0,"latency":{"50":0.001418144,"75":0.00180639025,"90":0.0070607187,"95":0.009561058849999996,"99":0.01523927838000001,"samples":1,"value":5e-07},"log_counts":{"info":9,"warn":40},"mem":19672,"ms_since_last_registry_update":1547,"num_cores":2,"rate":[1.1361328993362565,1.1344545494448148,1.1365784133171992],"requests":13832,"requests_per_sec":1.1361328993362565,"responses_2xx":13814,"responses_3xx":0,"responses_4xx":9,"responses_5xx":0,"responses_xxx":0,"start":"2016-01-07 19:04:40 +0000","tags":{"component":{"CloudController":{"latency":{"50":0.009015199,"75":0.0107408015,"90":0.015104917100000005,"95":0.01916497394999999,"99":0.034486261410000024,"samples":1,"value":5e-07},"rate":[0.13613289933245148,0.13433569936308343,0.13565885617276216],"requests":1686,"responses_2xx":1684,"responses_3xx":0,"responses_4xx":2,"responses_5xx":0,"responses_xxx":0},"HM9K":{"latency":{"50":0.0033354,"75":0.00751815875,"90":0.011916812100000005,"95":0.013760064,"99":0.013760064,"samples":1,"value":5e-07},"rate":[1.6850238803894876e-12,5.816129919395257e-05,0.00045864309255845694],"requests":12,"responses_2xx":6,"responses_3xx":0,"responses_4xx":6,"responses_5xx":0,"responses_xxx":0},"dea-0":{"latency":{"50":0.001354994,"75":0.001642107,"90":0.0020699939000000003,"95":0.0025553900499999996,"99":0.003677146940000006,"samples":1,"value":5e-07},"rate":[1.0000000000000013,1.0000000002571303,0.9999994853579043],"requests":12103,"responses_2xx":12103,"responses_3xx":0,"responses_4xx":0,"responses_5xx":0,"responses_xxx":0},"uaa":{"latency":{"50":0.038288465,"75":0.245610809,"90":0.2877324668,"95":0.311816554,"99":0.311816554,"samples":1,"value":5e-07},"rate":[8.425119401947438e-13,2.9080649596976205e-05,0.00022931374141467497],"requests":17,"responses_2xx":17,"responses_3xx":0,"responses_4xx":0,"responses_5xx":0,"responses_xxx":0}}},"top10_app_requests":[{"application_id":"063f95f9-492c-456f-b569-737f69c04899","rpm":60,"rps":1}],"type":"Router","uptime":"0d:3h:22m:31s","urls":21,"uuid":"0-c7fd7d76-f8d8-46b7-7a1c-7a59bcf7e286"}
```

### Profiling the Server

The GoRouter runs the [debugserver](https://github.com/cloudfoundry/debugserver), which is a wrapper around the go pprof tool. In order to generate this profile, do the following:

```bash
# Establish a SSH tunnel to your server (not necessary if you can connect directly)
ssh -L localhost:8080:[INTERNAL_SERVER_IP]:17001 vcap@[BOSH_DIRECTOR]
# Run the profile tool.
go tool pprof http://localhost:8080/debug/pprof/profile
```

## Load Balancing

The GoRouter is, in simple terms, a reverse proxy that load balances between many backend instances. The default load balancing algorithm that GoRouter will use is a simple **round-robin** strategy. GoRouter will retry a request if the chosen backend does not accept the TCP connection.

### Round-Robin
Default load balancing algorithm that gorouter will use or may be explicity set in **gorouter.yml**
```yaml
default_balancing_algorithm: round-robin
```

### Least-Connection
The GoRouter also supports least connection based routing and this can be enabled in **gorouter.yml**
```yaml
default_balancing_algorithm: least-connection
```
Least connection based load balancing will select the endpoint with the least number of connections. If multiple endpoints match with the same number of least connections, it will select a random one within those least connections.

_NOTE: GoRouter currently only supports changing the load balancing strategy at the gorouter level and does not yet support a finer-grained level such as route-level. Therefore changing the load balancing algorithm from the default (round-robin) should be proceeded with caution._



## When terminating TLS in front of Gorouter with a component that does not support sending HTTP headers

### Enabling apps and CF to detect that request was encrypted using X-Forwarded-Proto
If you terminate TLS in front of Gorouter, your component should send the `X-Forwarded-Proto` HTTP header in order for applications and Cloud Foundry system components to correctly detect when the original request was encrypted. As an example, UAA will reject requests that do not include `X-Forwarded-Proto: https`.

If your TLS-terminating component does not support sending HTTP headers, we recommend also terminating TLS at Gorouter. In this scenario you should only disable TLS at Gorouter if your TLS-terminating component rejects unencrypted requests **and** your private network is completely trusted. In this case, use the following property to inform applications and CF system components that requests are secure.

```
properties:
  router:
    force_forwarded_proto_https: true
```

### Enabling apps to detect the requestor's IP address uing PROXY Protocol

If you terminate TLS in front of Gorouter, your component should also send the `X-Forwarded-Proto` HTTP header in order for  `X-Forwarded-For` header to applications can detect the requestor's IP address.

If your TLS-terminating component does not support sending HTTP headers, you can use the PROXY protocol to send Gorouter the requestor's IP address.

If your TLS-terminating component supports the PROXY protocol, enable the PROXY protocol on Gorouter using the following cf-release manifest property:

```
properties:
  router:
    enable_proxy: true
```

You can test this feature manually:

```
echo -e "PROXY TCP4 1.2.3.4 [GOROUTER IP] 12345 [GOROUTER PORT]\r\nGET / HTTP/1.1\r\nHost: [APP URL]\r\n" | nc [GOROUTER IP] [GOROUTER PORT]
```

You should see in the access logs on the GoRouter that the `X-Forwarded-For` header is `1.2.3.4`. You can read more about the PROXY Protocol [here](http://www.haproxy.org/download/1.5/doc/proxy-protocol.txt).

## HTTP/2 Support

The GoRouter does not currently support proxying HTTP/2 connections, even over TLS. Connections made using HTTP/1.1, either by TLS or cleartext, will be proxied to backends over cleartext.

## Logs

The router's logging is specified in its YAML configuration file. It supports the following log levels:

* `fatal` - A fatal error has occurred that makes gorouter unable to handle any requests.
Examples: the router can't bind to its TCP port, a CF component has published invalid data to the router.
* `error` - An unexpected error has occurred. Examples: the router failed to fetch token from UAA service.
* `info`, `debug` - An expected event has occurred. Examples: a new CF component was registered with the router, the router has begun
to prune routes for stale droplets.

Sample log message in gorouter.

`[2017-02-01 22:54:08+0000] {"log_level":0,"timestamp":1485989648.0895808,"message":"endpoint-registered","source":"vcap.gorouter.registry","data":{"uri":"0-*.login.bosh-lite.com","backend":"10.123.0.134:8080","modification_tag":{"guid":"","index":0}}}
`

- `log_level`: This represents logging level of the message
- `timestamp`: Epoch time of the log
- `message`: Content of the log line
- `source`: The function within Gorouter that initiated the log message
- `data`: Additional information that varies based on the message

Access logs provide information for the following fields when recieving a request:

`<Request Host> - [<Start Date>] "<Request Method> <Request URL> <Request Protocol>" <Status Code> <Bytes Received> <Bytes Sent> "<Referer>" "<User-Agent>" <Remote Address> <Backend Address> x_forwarded_for:"<X-Forwarded-For>" x_forwarded_proto:"<X-Forwarded-Proto>" vcap_request_id:<X-Vcap-Request-ID> response_time:<Response Time> app_id:<Application ID> app_index:<Application Index> <Extra Headers>`
* Status Code, Response Time, Application ID, Application Index, and Extra Headers are all optional fields
* The absence of Status Code, Response Time, Application ID, or Application Index will result in a "-" in the corresponding field

Access logs are also redirected to syslog.

## Headers

If an user wants to send requests to a specific app instance, the header `X-CF-APP-INSTANCE` can be added to indicate the specific instance to be targeted. The format of the header value should be `X-Cf-App-Instance: APP_GUID:APP_INDEX`. If the instance cannot be found or the format is wrong, a 404 status code is returned. Usage of this header is only available for users on the Diego architecture.

## Supported Cipher Suites

The Gorouter supports both RFC and OpenSSL formatted values. Refer to [golang 1.7](https://github.com/golang/go/blob/release-branch.go1.7/src/crypto/tls/cipher_suites.go#L269-L285) for the list of supported cipher suites for Gorouter. Refer to [this documentation](https://testssl.sh/openssl-rfc.mapping.html) for a list of OpenSSL RFC mappings.
Example configurations enabling the TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 cipher suite for Gorouter:

```
...
enable_ssl: true
cipher_suite: "ECDHE-ECDSA-AES128-GCM-SHA256"
...
```

or

```
...
enable_ssl: true
cipher_suite: "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"
...
```


## Docs

There is a separate [docs](docs) folder which contains more advanced topics.

## Troubleshooting

Refer [doc](https://docs.pivotal.io/pivotalcf/1-9/adminguide/troubleshooting_slow_requests.html) to learn more troubleshooting slow requests.

## Contributing

Please read the [contributors' guide](https://github.com/cloudfoundry/gorouter/blob/master/CONTRIBUTING.md)
Please read our [Development Guide for Gorouter](https://github.com/cloudfoundry/gorouter/blob/master/docs/gorouter_development_guide.md)
