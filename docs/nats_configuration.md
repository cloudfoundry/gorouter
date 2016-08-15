## Consistency over Availability:

In the event that a NATS cluster goes down or becomes unavailable, `gorouter`
will attempt to reconnect to all configured NATS servers. If its unable to do
so within [`droplet_stale_threshold`](../config/config.go#L105) value, it will
drop the routes from its routing table. This strategy supports consistency over
availability.

In a catastrophic scenario where none of the configured NATS servers are
responding, gorouter will not retain any routing information and applications
will not be routable. However if an operator would prefer to keep applications
routable in the event of a NATS cluster outage, there is an option.

Opt-in config [`suspend_pruning_if_nats_unavailable`](../config/config.go#L103)
to suspend route pruning if `gorouter` cannot connect to NATS servers. This
config option will set max reconnect in NATS client to -1 (no limit) which
ensures at any point if NATS is reachable, `gorouter` does not prune the routes.
This strategy favors availability over consistency.

Default behavior will remain as pruning routes.

>**Warning**: There is a possibility of routing to an incorrect endpoint in the case
>of port re-use. To be used with caution.

## Relation between DropletStaleThreshold, NATs PingInterval and MinimumRegistrationInterval

### Definitions:

DropletStaleThreshold: Time after which `gorouter` considers the route
information as stale.
NATS PingInterval: Interval configured by NATS client to
ping configured NATS servers.
MinimumRegistrationInterval: Expect interval for
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
