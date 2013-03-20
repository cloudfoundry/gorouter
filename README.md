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


## Install from source

    $ git clone https://github.com/cloudfoundry/gorouter.git
    $ cd gorouter
    $ git submodule init
    $ git submodule update
    $ ./bin/go install router/router
    $ gem install nats

## Start gorouter

    $ nats-server
    $ ./bin/router

## Use gorouter

Periodically send `router.register` message :

    $ nats-pub 'router.register' '{ "dea": "974f4d94991a9f04f0277b9696cd785d", "host": "localhost", "port": 3000, "uris": [ "helloworld.vcap.me" ], "tags": { "framework": "sinatra", "runtime": "ruby18" } }'
    Published [router.register] : '{ "dea": "974f4d94991a9f04f0277b9696cd785d", "host": "localhost", "port": 3000, "uris": [ "helloworld.vcap.me" ], "tags": { "framework": "sinatra", "runtime": "ruby18" } }'

Now you can access your webapp

    $ curl helloworld.vcap.me:808

## Notes

* 03/05/13: Code is now used on CloudFoundry.com.

* 1/25/13: The code in this repository has not yet been used on CloudFoundry.com.

* 1/25/13: While this implementation can easily support WebSocket
  connections it does not yet.
