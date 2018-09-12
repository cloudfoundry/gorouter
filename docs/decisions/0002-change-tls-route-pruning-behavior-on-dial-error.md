# 2. Change TLS route pruning behavior on dial error

Date: 2018-09-12

## Status

Proposed

## Context

We had received a bug noting that when route-integrity is
turned on we do not prune routes that receive a `Dial` error.
Looking through the code we have special logic in place making
it so that there are only two types of errors that result in a
prune:

[errors on which we can prune](https://github.com/cloudfoundry/gorouter/blob/b63e6fb16c2a422ec5108a19debc9adb81f2d1dd/proxy/fails/classifier_group.go#L17-L20)

The prune operation should be harmless - route will get added again
(if the application is still around) the next time the
route-registrar runs.

[current code behavior with classifier](https://github.com/cloudfoundry/gorouter/blob/b63e6fb16c2a422ec5108a19debc9adb81f2d1dd/route/pool.go#L369-L372)

## Decision

We are going to start pruning any TLS route that has had a dial
failure immediately.

We will also be logging the cases where we are pruning versus
marking an endpoint as failed and removing it from the route
pool for a 'cooldown'.

## Consequences

If developers app is not coming up they may start to see a new
pattern of 502 followed by a series of 404s (until the route
is re-emitted).

Logging will be introduced into the route pool for the first time
giving operators a view into whether a prune or fail has actually
occurred and what error caused it.
