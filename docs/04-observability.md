---
title: Observability
expires_at: never
tags: [ routing-release,gorouter ]
---

# Observability

## Instrumentation

### The Routing Table

The `/routes` endpoint returns the entire routing table as JSON. This endpoint
requires basic authentication and is served on port `8082`. This port is configurable
via the `router.status.routes.port` property in the BOSH deployment manifest, or via
the `status.routes.port` property in `/var/vcap/jobs/gorouter/config/gorouter.yml`.
Route information is available via localhost only.

Each route has an associated array of host:port entries, formatted as follows:

```bash
$ curl "http://someuser:somepass@localhost:8080/routes"
{
  "api.catwoman.cf-app.com": [
    {
      "address": "10.244.0.138:9022",
      "ttl": 0,
      "tags": {
        "component": "CloudController"
      }
    }
  ],
  "dora-dora.catwoman.cf-app.com": [
    {
      "address": "10.244.16.4:60035",
      "ttl": 0,
      "tags": {
        "component": "route-emitter"
      }
    },
    {
      "address": "10.244.16.4:60060",
      "ttl": 0,
      "tags": {
        "component": "route-emitter"
      }
    }
  ]
}
```

> [!NOTE]
> This endpoint is internal only, and may change in the future. To safeguard
against changes, rely on the `/var/vcap/jobs/gorouter/bin/retrieve-local-routes` script
to get this information.

Because of the nature of the data present in `/varz` and `/routes`, they require
http basic authentication credentials. These credentials can be found the BOSH
manifest for cf-deployment under the `router` job:

```bash
properties:
  router:
    status:
      password: zed292_bevesselled
      port:
      user: paronymy61-polaric
```

If `router.status.user` is not set in the manifest, the default is
`router-status` as can be seen from [the job
spec](https://github.com/cloudfoundry/routing-release/blob/develop/jobs/gorouter/spec).

Or on the Gorouter VM under `/var/vcap/jobs/gorouter/config/gorouter.yml`:

```yaml
status:
  port: 8080
  user: some_user
  pass: some_password
```

### Metrics (Envelope v1)

The `/varz` endpoint provides status and metrics. This endpoint requires basic
authentication.

<details>
  <summary>Metrics response (click to expand)</summary>

```bash
$ curl "http://someuser:somepass@10.0.32.15:8080/varz"
{
  "bad_gateways": 0,
  "bad_requests": 20,
  "cpu": 0,
  "credentials": [
    "user",
    "pass"
  ],
  "droplets": 26,
  "host": "10.0.32.15:8080",
  "index": 0,
  "latency": {
    "50": 0.001418144,
    "75": 0.00180639025,
    "90": 0.0070607187,
    "95": 0.009561058849999996,
    "99": 0.01523927838000001,
    "samples": 1,
    "value": 5e-07
  },
  "log_counts": {
    "info": 9,
    "warn": 40
  },
  "mem": 19672,
  "ms_since_last_registry_update": 1547,
  "num_cores": 2,
  "rate": [
    1.1361328993362565,
    1.1344545494448148,
    1.1365784133171992
  ],
  "requests": 13832,
  "requests_per_sec": 1.1361328993362565,
  "responses_2xx": 13814,
  "responses_3xx": 0,
  "responses_4xx": 9,
  "responses_5xx": 0,
  "responses_xxx": 0,
  "start": "2016-01-07 19:04:40 +0000",
  "tags": {
    "component": {
      "CloudController": {
        "latency": {
          "50": 0.009015199,
          "75": 0.0107408015,
          "90": 0.015104917100000005,
          "95": 0.01916497394999999,
          "99": 0.034486261410000024,
          "samples": 1,
          "value": 5e-07
        },
        "rate": [
          0.13613289933245148,
          0.13433569936308343,
          0.13565885617276216
        ],
        "requests": 1686,
        "responses_2xx": 1684,
        "responses_3xx": 0,
        "responses_4xx": 2,
        "responses_5xx": 0,
        "responses_xxx": 0
      },
      "HM9K": {
        "latency": {
          "50": 0.0033354,
          "75": 0.00751815875,
          "90": 0.011916812100000005,
          "95": 0.013760064,
          "99": 0.013760064,
          "samples": 1,
          "value": 5e-07
        },
        "rate": [
          1.6850238803894876e-12,
          5.816129919395257e-05,
          0.00045864309255845694
        ],
        "requests": 12,
        "responses_2xx": 6,
        "responses_3xx": 0,
        "responses_4xx": 6,
        "responses_5xx": 0,
        "responses_xxx": 0
      },
      "dea-0": {
        "latency": {
          "50": 0.001354994,
          "75": 0.001642107,
          "90": 0.0020699939000000003,
          "95": 0.0025553900499999996,
          "99": 0.003677146940000006,
          "samples": 1,
          "value": 5e-07
        },
        "rate": [
          1.0000000000000013,
          1.0000000002571303,
          0.9999994853579043
        ],
        "requests": 12103,
        "responses_2xx": 12103,
        "responses_3xx": 0,
        "responses_4xx": 0,
        "responses_5xx": 0,
        "responses_xxx": 0
      },
      "uaa": {
        "latency": {
          "50": 0.038288465,
          "75": 0.245610809,
          "90": 0.2877324668,
          "95": 0.311816554,
          "99": 0.311816554,
          "samples": 1,
          "value": 5e-07
        },
        "rate": [
          8.425119401947438e-13,
          2.9080649596976205e-05,
          0.00022931374141467497
        ],
        "requests": 17,
        "responses_2xx": 17,
        "responses_3xx": 0,
        "responses_4xx": 0,
        "responses_5xx": 0,
        "responses_xxx": 0
      }
    }
  },
  "top10_app_requests": [
    {
      "application_id": "063f95f9-492c-456f-b569-737f69c04899",
      "rpm": 60,
      "rps": 1
    }
  ],
  "type": "Router",
  "uptime": "0d:3h:22m:31s",
  "urls": 21,
  "uuid": "0-c7fd7d76-f8d8-46b7-7a1c-7a59bcf7e286"
}
```

</details>

### Metrics (Prometheus)

The `<HOST>:<PROMETHEUS_PORT>/metrics` endpoint provides prometheus metrics.

Prometheus port, as well as certificates and other parameters, are configured in gorouter configuration.

<details>
  <summary>Metrics response (click to expand)</summary>

```bash
$ curl localhost:8042/metrics
# HELP backend_exhausted_conns number of errors related to backend connection limit reached
# TYPE backend_exhausted_conns counter
backend_exhausted_conns 0
# HELP backend_invalid_id number of bad backend id errors received from backends
# TYPE backend_invalid_id counter
backend_invalid_id 0
# HELP backend_invalid_tls_cert number of tls certificate errors received from backends
# TYPE backend_invalid_tls_cert counter
backend_invalid_tls_cert 0
# HELP backend_tls_handshake_failed number of backend handshake errors
# TYPE backend_tls_handshake_failed counter
backend_tls_handshake_failed 0
# HELP bad_gateways number of bad gateway errors received from backends
# TYPE bad_gateways counter
bad_gateways 0
# HELP empty_content_length_header number of requests with the empty content length header
# TYPE empty_content_length_header counter
empty_content_length_header 0
# HELP latency routing response latency in ms
# TYPE latency histogram
latency_bucket{component="",le="1"} 2
latency_bucket{component="",le="2"} 8705
latency_bucket{component="",le="4"} 55939
latency_bucket{component="",le="6"} 73786
latency_bucket{component="",le="8"} 79800
latency_bucket{component="",le="10"} 81911
latency_bucket{component="",le="20"} 86670
latency_bucket{component="",le="40"} 95701
latency_bucket{component="",le="50"} 100139
latency_bucket{component="",le="100"} 102135
latency_bucket{component="",le="500"} 104114
latency_bucket{component="",le="1000"} 104518
latency_bucket{component="",le="+Inf"} 104519
latency_sum{component=""} 1.5821502875950185e+06
latency_count{component=""} 104519
latency_bucket{component="CloudController",le="1"} 0
latency_bucket{component="CloudController",le="2"} 0
latency_bucket{component="CloudController",le="4"} 7
latency_bucket{component="CloudController",le="6"} 3928
latency_bucket{component="CloudController",le="8"} 5142
latency_bucket{component="CloudController",le="10"} 5606
latency_bucket{component="CloudController",le="20"} 22662
latency_bucket{component="CloudController",le="40"} 75564
latency_bucket{component="CloudController",le="50"} 82497
latency_bucket{component="CloudController",le="100"} 87240
latency_bucket{component="CloudController",le="500"} 91080
latency_bucket{component="CloudController",le="1000"} 91566
latency_bucket{component="CloudController",le="+Inf"} 91579
latency_sum{component="CloudController"} 3.744289433603952e+06
latency_count{component="CloudController"} 91579
latency_bucket{component="route-emitter",le="1"} 0
latency_bucket{component="route-emitter",le="2"} 337
latency_bucket{component="route-emitter",le="4"} 448
latency_bucket{component="route-emitter",le="6"} 462
latency_bucket{component="route-emitter",le="8"} 1241
latency_bucket{component="route-emitter",le="10"} 31250
latency_bucket{component="route-emitter",le="20"} 75412
latency_bucket{component="route-emitter",le="40"} 75695
latency_bucket{component="route-emitter",le="50"} 75711
latency_bucket{component="route-emitter",le="100"} 75717
latency_bucket{component="route-emitter",le="500"} 75799
latency_bucket{component="route-emitter",le="1000"} 75800
latency_bucket{component="route-emitter",le="+Inf"} 75802
latency_sum{component="route-emitter"} 808316.2385179874
latency_count{component="route-emitter"} 75802
latency_bucket{component="uaa",le="1"} 0
latency_bucket{component="uaa",le="2"} 1
latency_bucket{component="uaa",le="4"} 5951
latency_bucket{component="uaa",le="6"} 64736
latency_bucket{component="uaa",le="8"} 83650
latency_bucket{component="uaa",le="10"} 90136
latency_bucket{component="uaa",le="20"} 101690
latency_bucket{component="uaa",le="40"} 102816
latency_bucket{component="uaa",le="50"} 102868
latency_bucket{component="uaa",le="100"} 106615
latency_bucket{component="uaa",le="500"} 119313
latency_bucket{component="uaa",le="1000"} 119325
latency_bucket{component="uaa",le="+Inf"} 119328
latency_sum{component="uaa"} 2.5461252577970154e+06
latency_count{component="uaa"} 119328
# HELP ms_since_last_registry_update time since last registry update in ms
# TYPE ms_since_last_registry_update gauge
ms_since_last_registry_update 3942
# HELP promhttp_metric_handler_errors_total Total number of internal errors encountered by the promhttp metric handler.
# TYPE promhttp_metric_handler_errors_total counter
promhttp_metric_handler_errors_total{cause="encoding"} 0
promhttp_metric_handler_errors_total{cause="gathering"} 0
# HELP registry_message number of route registration messages
# TYPE registry_message counter
registry_message{action="added",component=""} 16
registry_message{action="added",component="CloudController"} 1
registry_message{action="added",component="route-emitter"} 2079
registry_message{action="added",component="uaa"} 4
registry_message{action="updated",component=""} 290636
registry_message{action="updated",component="CloudController"} 36330
registry_message{action="updated",component="route-emitter"} 1.218307e+06
registry_message{action="updated",component="uaa"} 145320
# HELP rejected_requests number of rejected requests
# TYPE rejected_requests counter
rejected_requests 1
# HELP responses number of responses
# TYPE responses counter
responses{status_group="2xx"} 312778
responses{status_group="3xx"} 29409
responses{status_group="4xx"} 49041
# HELP route_lookup_time route lookup time per request in ns
# TYPE route_lookup_time histogram
route_lookup_time_bucket{le="10000"} 377295
route_lookup_time_bucket{le="20000"} 389278
route_lookup_time_bucket{le="30000"} 390445
route_lookup_time_bucket{le="40000"} 391008
route_lookup_time_bucket{le="50000"} 391261
route_lookup_time_bucket{le="60000"} 391337
route_lookup_time_bucket{le="70000"} 391378
route_lookup_time_bucket{le="80000"} 391397
route_lookup_time_bucket{le="90000"} 391417
route_lookup_time_bucket{le="100000"} 391432
route_lookup_time_bucket{le="+Inf"} 391525
route_lookup_time_sum 1.584830859e+09
route_lookup_time_count 391525
# HELP route_registration_latency route registration latency in ms
# TYPE route_registration_latency histogram
route_registration_latency_bucket{le="0.1"} 0
route_registration_latency_bucket{le="0.5"} 0
route_registration_latency_bucket{le="1"} 0
route_registration_latency_bucket{le="1.5"} 0
route_registration_latency_bucket{le="2"} 2
route_registration_latency_bucket{le="2.5"} 15
route_registration_latency_bucket{le="3"} 152
route_registration_latency_bucket{le="3.5"} 625
route_registration_latency_bucket{le="4"} 1372
route_registration_latency_bucket{le="+Inf"} 2012
route_registration_latency_sum 7695.366220000008
route_registration_latency_count 2012
# HELP routes_pruned number of pruned routes
# TYPE routes_pruned counter
routes_pruned 0
# HELP total_requests number of routing requests
# TYPE total_requests counter
total_requests{component="",is_routed_app="no"} 104419
total_requests{component="CloudController",is_routed_app="no"} 91579
total_requests{component="route-emitter",is_routed_app="no"} 75802
total_requests{component="uaa",is_routed_app="no"} 119328
# HELP total_routes number of total routes
# TYPE total_routes gauge
total_routes 54
# HELP unregistry_message number of unregister messages
# TYPE unregistry_message counter
unregistry_message{component="route-emitter"} 2012
# HELP websocket_failures websocket failure
# TYPE websocket_failures counter
websocket_failures 0
# HELP websocket_upgrades websocket upgrade to websocket
# TYPE websocket_upgrades counter
websocket_upgrades 0
```

</details>

### Profiling the Server

The Gorouter runs the
[debugserver](https://github.com/cloudfoundry/debugserver), which is a wrapper
around the go pprof tool. In order to generate this profile, do the following:

```bash
# Establish a SSH tunnel to your server (not necessary if you can connect directly)
ssh -L localhost:8080:[INTERNAL_SERVER_IP]:17001 vcap@[BOSH_DIRECTOR]
# Run the profile tool.
go tool pprof http://localhost:8080/debug/pprof/profile
```

## Logs

The router's logging is specified in its YAML configuration file. It supports
the following log levels:

* `fatal` - A fatal error has occurred that makes gorouter unable to handle any
  requests. Examples: the router can't bind to its TCP port, a CF component has
  published invalid data to the router.
* `error` - An unexpected error has occurred. Examples: the router failed to
  fetch token from UAA service.
* `info` - An expected event has occurred. Examples: the router started or
  exited, the router has begun to prune routes for stale droplets.
* `debug` - A lower-level event has occurred. Examples: route registration,
  route unregistration.

Sample log message in gorouter.

`[2017-02-01 22:54:08+0000] {"log_level":0,"timestamp":"2019-11-21T22:16:18.750673404Z","message":"endpoint-registered","source":"vcap.gorouter.registry","data":{"uri":"0-*.login.bosh-lite.com","backend":"10.123.0.134:8080","modification_tag":{"guid":"","index":0}}}`

- `log_level`: This represents logging level of the message
- `timestamp`: Time of the log in either RFC 3339 (default) or epoch format
- `message`: Content of the log line
- `source`: The function within Gorouter that initiated the log message
- `data`: Additional information that varies based on the message

### Route table change logs

The following log messages are emitted any time the routing table changes:

- `route-registered`: a new route is added to the table
- `route-unregistered`: an existing route is removed from the table
- `endpoint-registered`: a new backend is added to the table
  e.g. an app is scaled up and a new app instance is started
- `endpoint-unregistered`: a backend is removed from the table
  e.g. an app is scaled down and an app instance is stopped

Examples:

Route mapped to existing application with 1 app instance:

```
{"log_level":1,"timestamp":"2020-08-27T22:59:43.462087363Z","message":"route-registered","source":"vcap.gorouter.registry","data":{"uri":"a.springgreen.cf-app.com"}}
{"log_level":1,"timestamp":"2020-08-27T22:59:43.462279999Z","message":"endpoint-registered","source":"vcap.gorouter.registry","data":{"uri":"a.springgreen.cf-app.com","backend":"10.0.1.11:61002","modification_tag":{"guid":"","index":0},"isolation_segment":"-","isTLS":true}}
```

App with two mapped routes scaled up from 1 instance to 2:

```
{"log_level":1,"timestamp":"2020-08-27T22:59:59.350998043Z","message":"endpoint-registered","source":"vcap.gorouter.registry","data":{"uri":"a.springgreen.cf-app.com","backend":"10.0.1.11:61006","modification_tag":{"guid":"","index":0},"isolation_segment":"-","isTLS":true}}
{"log_level":1,"timestamp":"2020-08-27T22:59:59.351131999Z","message":"endpoint-registered","source":"vcap.gorouter.registry","data":{"uri":"foo.springgreen.cf-app.com","backend":"10.0.1.11:61006","modification_tag":{"guid":"","index":0},"isolation_segment":"-","isTLS":true}}
```

App with two mapped routes scaled down from 2 instances to 1:

```
{"log_level":1,"timestamp":"2020-08-27T23:00:27.122616625Z","message":"endpoint-unregistered","source":"vcap.gorouter.registry","data":{"uri":"a.springgreen.cf-app.com","backend":"10.0.1.11:61006","modification_tag":{"guid":"","index":0},"isolation_segment":"-","isTLS":true}}
{"log_level":1,"timestamp":"2020-08-27T23:00:27.123043785Z","message":"endpoint-unregistered","source":"vcap.gorouter.registry","data":{"uri":"foo.springgreen.cf-app.com","backend":"10.0.1.11:61006","modification_tag":{"guid":"","index":0},"isolation_segment":"-","isTLS":true}}
```

Route unmapped from application with 1 app instance:

```
{"log_level":1,"timestamp":"2020-08-27T23:00:46.702876112Z","message":"endpoint-unregistered","source":"vcap.gorouter.registry","data":{"uri":"a.springgreen.cf-app.com","backend":"10.0.1.11:61002","modification_tag":{"guid":"","index":0},"isolation_segment":"-","isTLS":true}}
{"log_level":1,"timestamp":"2020-08-27T23:00:46.703133349Z","message":"route-unregistered","source":"vcap.gorouter.registry","data":{"uri":"a.springgreen.cf-app.com"}}
```

### Access logs

Access logs provide information for the following fields when receiving a
request:

`<Request Host> - [<Start Date>] "<Request Method> <Request URL>
<Request Protocol>" <Status Code> <Bytes Received> <Bytes Sent>
"<Referer>" "<User-Agent>" <Remote Address> <Backend Address>
x_forwarded_for:"<X-Forwarded-For>" x_forwarded_proto:"<X-Forwarded-Proto>"
vcap_request_id:<X-Vcap-Request-ID> response_time:<Response Time>
gorouter_time:<Gorouter Time> app_id:<Application ID>
app_index:<Application Index> instance_id:"<Instance ID>"
failed_attempts:<Failed Attempts> failed_attempts_time:<Failed Attempts Time>
dns_time:<DNS Time> dial_time:<Dial Time> tls_time:<TLS Time>
backend_time:<Backend Time> x_cf_routererror:<X-Cf-RouterError>
<Extra Headers>`

* Status Code, Response Time, Gorouter Time, Application ID, Application Index,
  X-Cf-RouterError, and Extra Headers are all optional fields. The absence of
  Status Code, Response Time, Application ID, Application Index, or
  X-Cf-RouterError will result in a "-" in the corresponding field.

* `Response Time` is the total time it takes for the request to go through the
  Gorouter to the app and for the response to travel back through the Gorouter.
  This includes the time the request spends traversing the network to the app
  and back again to the Gorouter. It also includes the time the app spends
  forming a response.

* `Gorouter Time` is the total time it takes for the request to go through the
  Gorouter initially plus the time it takes for the response to travel back
  through the Gorouter. This does not include the time the request spends
  traversing the network to the app. This also does not include the time the app
  spends forming a response.

* `failed_attempts`, `failed_attempts_time`, `dns_time`, `dial_time`,
  `tls_time` and `backend_time` are only logged if
  `logging.enable_attempts_details` is set to true. The `*_time` will only be
  provided for the last, successful attempt, if the request fails they will be
  empty and the error log can be consulted to get the details about each
  attempt. `failed_attempts_time` contains the total time spent performing
  attempts that failed.

* `X-CF-RouterError` is populated if the Gorouter encounters an error. This can
  help distinguish if a non-2xx response code is due to an error in the Gorouter
  or the backend. For more information on the possible Router Error causes go to
  the [#router-errors](03-features.md#router-errors) section.

Access logs are also redirected to syslog.
