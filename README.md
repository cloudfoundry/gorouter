[![Build Status](https://travis-ci.org/cloudfoundry/gorouter.png)](https://travis-ci.org/cloudfoundry/gorouter)

# gorouter

This repository contains the source code for a Go implementation of the Cloud
Foundry router.

This router is now used on CloudFoundry.com, replacing the old implementation.

## Summary

The original router can be found at cloudfoundry/router. The original router is
backed by nginx, that uses Lua code to connect to a Ruby server that -- based
on the headers of a client's request -- will tell nginx whick backend it should
use. The main limitations in this architecture are that nginx does not support
non-HTTP (e.g. traffic to services) and non-request/response type traffic (e.g.
to support WebSockets), and that it requires a round trip to a Ruby server for
every request.

The Go implementation of the Cloud Foundry router is an attempt in solving
these limitations. First, with full control over every connection to the
router, it can more easily support WebSockets, and other types of traffic (e.g.
via HTTP CONNECT). Second, all logic is contained in a single process,
removing unnecessary latency.

## Getting started

The following instructions may help you get started with gorouter in a
standalone environment.

### Setup

```
git clone https://github.com/cloudfoundry/gorouter.git
cd gorouter
git submodule update --init
./bin/go install router/router
gem install nats
```

### Start

```
# Start NATS server in daemon mode
nats-server -d

# Start gorouter
./bin/router
```

### Usage

When gorouter is used in Cloud Foundry, it receives route updates via NATS.
Routes that haven't been updated in 2 minutes (by default) are pruned.
Therefore, to maintain an active route, it needs to be updated at least every 2 minutes.
The format of these route updates are as follows:

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
  }
}
```

Such a message can be sent to both the `router.register` subject to register
URIs, and to the `router.unregister` subject to unregister URIs, respectively.

```
$ nohup ruby -rsinatra -e 'get("/") { "Hello!" }' &
$ nats-pub 'router.register' '{"host":"127.0.0.1","port":4567,"uris":["my_first_url.vcap.me","my_second_url.vcap.me"],"tags":{"another_key":"another_value","some_key":"some_value"}}'
Published [router.register] : '{"host":"127.0.0.1","port":4567,"uris":["my_first_url.vcap.me","my_second_url.vcap.me"],"tags":{"another_key":"another_value","some_key":"some_value"}}'
$ curl my_first_url.vcap.me:8080
Hello!
```

## Notes

* 03/05/13: Code is now used on CloudFoundry.com.

* 1/25/13: The code in this repository has not yet been used on CloudFoundry.com.

* 1/25/13: While this implementation can easily support WebSocket
  connections it does not yet.
