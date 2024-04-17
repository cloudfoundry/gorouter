# Troubleshooting gorouter

## Profiling and Tracing

A pprof endpoint is exposed via the [debugserver](https://github.com/cloudfoundry/debugserver)
package and can be configured using the `debug_addr` property. The endpoint allows operators to
enable profiling or tracing and extract data without changing gorouter itself. The
[net/http/pprof](https://pkg.go.dev/net/http/pprof#hdr-Usage_examples) package contains examples on
how to collect the various profiling / tracing data.

After collecting data it can be visualized using the `go` CLI. For best results you should copy the
gorouter binary which was used to collect the profiles. When working with profiles the results can
be displayed in a browser like so:

```
go tool pprof -http localhost:8081 ./gorouter ./profile0.out
```

Use `go tool pprof -h` to view all available options.

The web server will listen on localhost on port 8081 and your default browser will be launched to
display the results.

When collecting and analyzing traces the results can be viewed like so:

```
go tool trace ./trace0.out
```

Note: HTML based UI can be viewed in any browser, but the trace viewer is from the Chrome/Chromium
project and might not work on other browsers.

Use `go tool trace -h` to view all available options.

Gorouter already contains a set of trace points as there is only little built-in tracing in the go
standard library and code has to explicitly instrumented. The tracing currently focuses on request
handling and operations on the route registry. Each request is record as task with the name
`request [vcap-id]` and each route (un-)register message is recorded by its route key.

Other resources:
- [The Go Blog: Profiling Go Programs](https://go.dev/blog/pprof)
- [runtime/trace](https://pkg.go.dev/runtime/trace)
