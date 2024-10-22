# Features

## Dynamic Routing Table

Gorouters routing table is updated dynamically via the NATS message bus.  NATS
can be deployed via BOSH with
([cf-deployment](https://github.com/cloudfoundry/cf-deployment)) or standalone
using [nats-release](https://github.com/cloudfoundry/nats-release).

To add or remove a record from the routing table, a NATS client must send
register or unregister messages. Records in the routing table have a maximum TTL
of 120 seconds, so clients must heartbeat registration messages periodically; we
recommend every 20s.  [Route
Registrar](https://github.com/cloudfoundry/route-registrar) is a BOSH job that
comes with [Routing Release](https://github.com/cloudfoundry/routing-release)
that automates this process.

When deployed with Cloud Foundry, registration of routes for apps pushed to CF
occurs automatically without user involvement. For details, see [Routes and
Domains]
(https://docs.cloudfoundry.org/devguide/deploy-apps/routes-domains.html).

### Registering Routes via NATS

When the gorouter starts, it sends a `router.start` message to NATS.  This
message contains an interval that other components should then send
`router.register` on, `minimumRegisterIntervalInSeconds`. It is recommended that
clients should send `router.register` messages on this interval.  This
`minimumRegisterIntervalInSeconds` value is configured through the
`start_response_delay_interval` configuration property. Gorouter will prune
routes that it considers to be stale based upon a separate "staleness" value,
`droplet_stale_threshold`, which defaults to 120 seconds. Gorouter will check if
routes have become stale on an interval defined by
`prune_stale_droplets_interval`, which defaults to 30 seconds. All of these
values are represented in seconds and will always be integers.

The format of the `router.start` message is as follows:

```json
{
  "id": "some-router-id",
  "hosts": ["1.2.3.4"],
  "minimumRegisterIntervalInSeconds": 20,
  "prunteThresholdInSeconds": 120
}
```

After a `router.start` message is received by a client, the client should send
`router.register` messages. This ensures that the new router can update its
routing table and synchronize with existing routers.

If a component comes online after the router, it must make a NATS request called
`router.greet` in order to determine the interval. The response to this message
will be the same format as `router.start`.

The format of the `router.register` message is as follows:

```json
{
  "host": "127.0.0.1",
  "port": 4567,
  "tls_port": 1234,
  "protocol": "http1",
  "uris": [
    "my_first_url.localhost.routing.cf-app.com",
    "my_second_url.localhost.routing.cf-app.com"
  ],
  "tags": {
    "another_key": "another_value",
    "some_key": "some_value"
  },
  "app": "some_app_guid",
  "stale_threshold_in_seconds": 120,
  "private_instance_id": "some_app_instance_id",
  "isolation_segment": "some_iso_seg_name",
  "server_cert_domain_san": "some_subject_alternative_name"
}
```

`stale_threshold_in_seconds` is the custom staleness threshold for the route
being registered. If this value is not sent, it will default to the router's
default staleness threshold.

`app` is a unique identifier for an application that the endpoint is registered
for. This value will be included in router access logs with the label `app_id`,
as well as being sent with requests to the endpoint in an HTTP header
`X-CF-ApplicationId`.

`private_instance_id` is a unique identifier for an instance associated with the
app identified by the `app` field. Gorouter includes an HTTP header
`X-CF-InstanceId` set to this value with requests to the registered endpoint.

`isolation_segment` determines which routers will register route. Only Gorouters
configured with the matching isolation segment will register the route.  If a
value is not provided, the route will be registered only by Gorouters set to the
`all` or `shared-and-segments` router table sharding modes.  Refer to the job
properties for [Gorouter]
(https://github.com/cloudfoundry/routing-release/blob/develop/jobs/gorouter/spec)
for more information.

`tls_port` is the port that Gorouter will use to attempt TLS connections with
the registered backends. Supported only when `router.backend.enable_tls: true`
is configured in the manifest. `router.ca_certs` may be optionally configured
with a CA, for backends certificates signed by custom CAs. For mutual
authentication with backends, `router.backends.tls_pem` may be optionally
provided. When `router.backend.enable_tls: true`, Gorouter will prefer
`tls_port` over `port` if present in the NATS message. Otherwise, `port` will be
preferred, and messages with only `tls_port` will be rejected and an error
message logged.

`server_cert_domain_san` (required when `tls_port` is present) Indicates a
string that Gorouter will look for in a Subject Alternative Name (SAN) of the
TLS certificate hosted by the backend to validate instance identity. When the
value of `server_cert_domain_san` does not match a SAN in the server
certificate, Gorouter will prune the backend and retry another backend for the
route if one exists, or return a 503 if it cannot validate the identity of any
backend in three tries.

Additionally, if the `host` and `tls_port` pair matches an already registered
`host` and `port` pair, the previously registered route will be overwritten and
Gorouter will now attempt TLS connections with the `host` and `tls_port` pair.
The same is also true if the `host` and `port` pair matches an already
registered `host` and `tls_port` pair, except Gorouter will no longer attempt
TLS connections with the backend.

Such a message can be sent to both the `router.register` subject to register
URIs, and to the `router.unregister` subject to unregister URIs, respectively.

### Deleting a Route

Routes can be deleted with the `router.unregister` nats message. The format of
the `router.unregister` message the same as the `router.register` message, but
most information is ignored. Any route that matches the `host`, `port` and
`uris` fields will be deleted.

### Example

Create a simple app
```bash
$ nohup ruby -rsinatra -e 'get("/") { "Hello!" }' &
```

Send a register message
```bash
$ nats-pub 'router.register' '{"host":"127.0.0.1","port":4567,"uris":["my_first_url.localhost.routing.cf-app.com","my_second_url.localhost.routing.cf-app.com"],"tags":{"another_key":"another_value","some_key":"some_value"}}'

Published [router.register] : '{"host":"127.0.0.1","port":4567,"uris":["my_first_url.localhost.routing.cf-app.com","my_second_url.localhost.routing.cf-app.com"],"tags":{"another_key":"another_value","some_key":"some_value"}}'
```

See that it works!
```bash
$ curl my_first_url.localhost.routing.cf-app.com:8081
Hello!
```

Unregister the route
```bash
$ nats-pub 'router.unregister' '{"host":"127.0.0.1","port":4567,"tls_port":1234,"uris":["my_first_url.localhost.routing.cf-app.com","my_second_url.localhost.routing.cf-app.com"]}'

Published [router.unregister] : '{"host":"127.0.0.1","port":4567,"tls_port":1234,"uris":["my_first_url.localhost.routing.cf-app.com","my_second_url.localhost.routing.cf-app.com"]}'
```

See that the route is gone

```bash
$ curl my_first_url.localhost.routing.cf-app.com:8081
404 Not Found: Requested route ('my_first_url.localhost.routing.cf-app.com') does not exist.
```

If `router.backends.enable_tls` has been set to true, `tls_port` will be used as
the definitive port when unregistering a route if present, otherwise `port` will
be used. If `router.backends.enable_tls` is set to false, `port` will be
preferred and any requests with only `tls_port` will be rejected and an error
logged to the gorouter logs.

Note that if `router.backends.enable_tls` is true and `host` and `tls_port`
happens to match a registered `host` and `port` pair, this `host` and `port`
pair will be unregistered. The reverse is also true.

> **Note:** In order to use `nats-pub` to register a route, you must install the
> [gem](https://github.com/nats-io/ruby-nats) on a Cloud Foundry VM. It's
> easiest on a VM that has ruby as a package, such as the API VM. Find the ruby
> installed in `/var/vcap/packages`, export your PATH variable to include the bin
> directory, and then run `gem install nats`. Find the nats login info from your
> gorouter config and use it to connect to the nats cluster.

## Health checking from a Load Balancer

To scale Gorouter horizontally for high-availability or throughput capacity, you
must deploy it behind a highly-available load balancer (F5, AWS ELB, etc).

Gorouter has a health endpoint `/health` on port 8443 (with TLS) and
on 8080 (without TLS) that returns a 200 OK which indicates the Gorouter instance
is healthy; any other response indicates unhealthy.  These port can be configured
via the `router.status.port` and `router.status.tls.port` properties in the BOSH
deployment manifest or via the `status.port` and `status.tls.port` properties
under `/var/vcap/jobs/gorouter/config/gorouter.yml`


```bash
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

**DEPRECATED:** Your load balancer can be configured to send an HTTP
healthcheck on port 80 with the `User-Agent` HTTP header set to
`HTTP-Monitor/1.1`. A 200 response indicates the Gorouter instance is healthy;
any other response indicates unhealthy. Gorouter can be configured to accept
alternate values for the User Agent header using the `healthcheck_user_agent`
configuration property; as an example, AWS ELBS send `User-Agent:
ELB-HealthChecker/1.0`.

```bash
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

**DEPRECATED:** The `/healthz` endpoint is now an alias for the `/health` endpoint
to ensure backward compatibility.

## Load Balancing

The Gorouter is, in simple terms, a reverse proxy that load balances between
many backend instances. The default load balancing algorithm that Gorouter will
use is a simple **round-robin** strategy. Gorouter will retry a request if the
chosen backend does not accept the TCP connection.

### Round-Robin
Default load balancing algorithm that gorouter will use or may be explicitly set
in **gorouter.yml** `yaml default_balancing_algorithm: round-robin`

### Least-Connection
The Gorouter also supports least connection based routing and this can be
enabled in **gorouter.yml**

```yaml
default_balancing_algorithm: least-connection
```

Least connection based load balancing will select the endpoint with the least
number of connections. If multiple endpoints match with the same number of least
connections, it will select a random one within those least connections.

_NOTE: Gorouter currently only supports changing the load balancing strategy at
the gorouter level and does not yet support a finer-grained level such as
route-level. Therefore changing the load balancing algorithm from the default
(round-robin) should be proceeded with caution._

## When terminating TLS in front of Gorouter with a component that does not support sending HTTP headers

### Enabling apps and CF to detect that request was encrypted using X-Forwarded-Proto

If you terminate TLS in front of Gorouter, your component should send the
`X-Forwarded-Proto` HTTP header in order for applications and Cloud Foundry
system components to correctly detect when the original request was encrypted.
As an example, UAA will reject requests that do not include `X-Forwarded-Proto:
https`.

If your TLS-terminating component does not support sending HTTP headers, we
recommend also terminating TLS at Gorouter. In this scenario you should only
disable TLS at Gorouter if your TLS-terminating component rejects unencrypted
requests **and** your private network is completely trusted. In this case, use
the following property to inform applications and CF system components that
requests are secure.

```yaml
properties:
  router:
    force_forwarded_proto_https: true
```

### Enabling apps to detect the requestor's IP address using PROXY Protocol

If you terminate TLS in front of Gorouter, your component should also send the
`X-Forwarded-Proto` HTTP header in order for `X-Forwarded-For` header to
applications can detect the requestor's IP address.

If your TLS-terminating component does not support sending HTTP headers, you can
use the PROXY protocol to send Gorouter the requestor's IP address.

If your TLS-terminating component supports the PROXY protocol, enable the PROXY
protocol on Gorouter using the following cf-deployment manifest property:

```yaml
properties:
  router:
    enable_proxy: true
```

You can test this feature manually:

```bash
echo -e "PROXY TCP4 1.2.3.4 [GOROUTER IP] 12345 [GOROUTER PORT]\r\nGET / HTTP/1.1\r\nHost: [APP URL]\r\n" | nc [GOROUTER IP] [GOROUTER PORT]
```

You should see in the access logs on the Gorouter that the `X-Forwarded-For`
header is `1.2.3.4`. You can read more about the PROXY Protocol
[here](http://www.haproxy.org/download/1.5/doc/proxy-protocol.txt).

## HTTP/2 Support

The Gorouter supports ingress and egress HTTP/2 connections when the BOSH
deployment manifest property is enabled.

```yaml
properties:
  router:
    enable_http2: true
```

By default, connections will be proxied to backends over HTTP/1.1, regardless of
ingress protocol. Backends can be configured with the `http2` protocol to enable
end-to-end HTTP/2 routing for use cases like gRPC.

Example `router.register` message with `http2` protocol:
```json
{
  "host": "127.0.0.1",
  "port": 4567,
  "protocol": "http2",
  "...": "..."
}
```

## Headers

If a user wants to send requests to a specific app instance, the header
`X-CF-APP-INSTANCE` can be added to indicate the specific instance to be
targeted. The format of the header value should be `X-Cf-App-Instance:
APP_GUID:APP_INDEX`. If the instance cannot be found or the format is wrong, a
400 status code is returned. In addition, Gorouter will return a
`X-Cf-Routererror` header.  If the instance guid provided is incorrectly
formatted, the value of the header will be `invalid_cf_app_instance_header`.  If
the instance guid provided is correctly formatted, but the guid does not exist,
the value of this header will be `unknown_route`, and the request body will
contain `400 Bad Request: Requested instance ('1') with guid
('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa') does not exist for route
('dora.superman.routing.cf-app.com')`.

Usage of the `X-Cf-App-Instance` header is only available for users on the Diego
architecture.

### Router Errors

The value of the `X-Cf-Routererror` header can be one of the following:

| Value                          | Description                                                                                                                                                                                                    |
|--------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| invalid_cf_app_instance_header | The provided value for the "X-Cf-App-Instance" header does not match the required format of `APP_GUID:INSTANCE_ID`.                                                                                            |
| empty_host                     | The value for the "Host" header is empty, or the "Host" header is equivalent to the remote address. Some LB's optimistically set the "Host" header value with their IP address when there is no value present. |
| unknown_route                  | The desired route does not exist in the gorouter's route table.                                                                                                                                                |
| no_endpoints                   | There is an entry in the route table for the desired route, but there are no healthy endpoints available.                                                                                                      |
| Connection Limit Reached       | The backends associated with the route have reached their max number of connections. The max connection number is set via the spec property `router.backends.max_conns`.                                       |
| route_service_unsupported      | Route services are not enabled. This can be configured via the spec property `router.route_services_secret`. If the property is empty, route services are disabled.                                            |
| endpoint_failure               | The registered endpoint for the desired route failed to handle the request.                                                                                                                                    |
