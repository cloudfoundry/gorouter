package test_util

// Our tests assume that there exists a DNS entry *.localhost.routing.cf-app.com that maps to 127.0.0.1
// If that DNS entry does not work, then many, many tests will fail
const LocalhostDNS = "localhost.routing.cf-app.com"
