## Consistency over Availability:

In the context of Cloud Foundry, when an application instance crashes or is stopped as a result of the app being stopped or scaled down, the allocated IP and port are released to the pool. The same IP and port may then be assigned to a new instance of another application, as when a new app is started, scaled up, or a crashed instance is recreated. Under normal operation, each of these events will result in a change to Gorouter's routing table. Updates to the routing table depend on a message being sent by a client to NATS (e.g. Route Emitter is responsible for sending changes to routing data for apps running on Diego), and on Gorouter fetching the message from NATS. 

If Gorouter loses its connection to NATS, it will attempt to reconnect to all servers in the NATS cluster. If it is unable to reconnect to any NATS server, and so is unable to receive changes to the routing table, connections for one application may be routed to an unintended one. We called these "stale routes," or say that the routing table is "stale." 

To prevent stale routes, Gorouter is by default optimized for consistency over availability. Each route has a TTL of 120 seconds ([see `droplet_stale_threshold`](../config/config.go#L105)), and clients are responsible for heartbeating registration of their routes. Each time Gorouter receives a heartbeat for a route, the TTL is reset. If Gorouter does not receive a heartbeat within the TTL, the route is pruned from the routing table. If all backends for a route are pruned, Gorouter will respond with a 404 to requests for the route. If Gorouter can't reach NATS, then all routes are pruned and Gorouter will respond with a 404 to all requests. This constitutes a total application outage.

If an operator would prefer to favor availability over consistency, the configuration property [`suspend_pruning_if_nats_unavailable`](../config/config.go#L103) can be used to ignore route TTL and prevent pruning in the event that Gorouter cannot connect to NATS. This config option will also set max reconnect in the NATS client to -1 (no limit) which prevents Gorouter from crashing and losing its in-memory routing table. This configuration option is set to false by default. 

>**Warning**: There is a significant probability of routing to an incorrect backend endpoint in the case of port re-use. Suspending route pruning should be used with caution.

## Relation between DropletStaleThreshold, NATs PingInterval and MinimumRegistrationInterval

### Definitions:

`DropletStaleThreshold`: Time after which `gorouter` considers the route
information as stale.

`NATS PingInterval`: Interval configured by NATS client to
ping configured NATS servers.

`MinimumRegistrationInterval`: Expect interval for
gorouter clients to send the routing info. (eg: [Route
Registrar](https://github.com/cloudfoundry-incubator/route-registrar))

In a deployment with multiple NATS servers, if one of the servers becomes
unhealthy `gorouter` should fail over to a healthy server(if any available)
before DropletStaleThreshold value is reached to avoid pruning routes.
Ping interval for NATS clients is calculated by the following equation:
```
PingInterval = (DropletStaleThreshold - (StartResponseDelayInterval +
minimumRegistrationInterval) - (NATS Timeout * NumberOfNatsServers))/3
```

 `(StartResponseDelayInterval + minimumRegistrationInterval)` : This part of the
 equation accounts for the startup delay during which `gorouter` doesn't accept
 any requests and the registration interval from `gorouter` clients.
 
 `(NATS Timeout * NumberOfNatsServers)` : This part of the equation takes number of
 NATS servers configured into account. Default connection timeout for NATS
 clients is 2 seconds.

 Currently we do not allow the operator to set the value for
 DropletStaleThreshold and StartResponseDelayInterval, hence there is no real
 need for the above equation to calculate the ping interval yet. After long
 consideration of different scenarios we have decided configure interval with value [`20` seconds](https://github.com/cloudfoundry/gorouter/blob/master/config/config.go#L199).
