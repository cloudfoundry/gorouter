# 2. Change TLS endpoint pruning behavior

Date: 2018-09-13

## Status

Accepted

## Context

This is related to story [#158847588](https://www.pivotaltracker.com/story/show/158847588)

Prior to the story above, when route-integrity was turned on (soon to be the
default) we did not prune routes that received most [retriable]() errors. The
code ensures that there are only two types of errors that [result in a
prune](https://github.com/cloudfoundry/gorouter/blob/b63e6fb16c2a422ec5108a19debc9adb81f2d1dd/route/pool.go#L369-L372):

[Hostname Mismatch and Attempted TLS with Non TLS
Backend](https://github.com/cloudfoundry/gorouter/blob/b63e6fb16c2a422ec5108a19debc9adb81f2d1dd/proxy/fails/classifier_group.go#L17-L20)

The prune operation should have little user impact - the route will get added
again the next time the route-registrar runs if the application is still
running.

## Decision

We will prune any TLS route that has had a failure immediately. Consequently,
we are immediately pruning on more errors, such that the final list includes the
following errors: AttemptedTLSWithNonTLSBackend, Dial, RemoteFailedCertCheck,
RemoteHandshakeFailure, HostnameMismatch, UntrustedCert

We will also add logging to the cases when an endpoint is pruned.

## Consequences

If a developer's app is flapping, they may start to see a new pattern: a 502
followed by a series of 404s (until the route is re-emitted).

Logging will be introduced into the route pool pruning behavior, giving
operators a view into whether a prune or fail has occurred and what error caused
it.

[As of this date, endpoint retry logic will not change](https://docs.cloudfoundry.org/concepts/http-routing.html#transparent)
