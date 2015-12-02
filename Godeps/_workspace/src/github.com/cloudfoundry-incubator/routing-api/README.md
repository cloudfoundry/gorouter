# CF Routing API Server (Experimental)

The initial release of this API server is currently in development and subject to backward incompatible changes.

The purpose of the Routing API is to present a RESTful interface for registering and deregistering routes for both internal and external clients. This allows easier consumption by different clients as well as the ability to register routes from outside of the CF deployment.

## Downloading and Installing

### External Dependencies

- Go should be installed and in the PATH
- This repo is part of [cf-routing-release](https://github.com/cloudfoundry-incubator/cf-routing-release) bosh release repo, which also acts as cannonical GOPATH. So to work on routing-api you will need to checkout [cf-routing-release](https://github.com/cloudfoundry-incubator/cf-routing-release) and follow instructions in its [README](https://github.com/cloudfoundry-incubator/cf-routing-release/blob/develop/README.md) to setup GOPATH.


### Development Setup

Refer to cf-routing-release [README](https://github.com/cloudfoundry-incubator/cf-routing-release/blob/develop/README.md) for development setup.

## Development

### etcd

To run the tests you need a running etcd cluster on version 2.1.1. To get that do:

```sh
go get github.com/coreos/etcd
cd $GOPATH/src/github.com/coreos/etcd
git fetch --tags
git checkout v2.1.1
go install .
```

Once installed, you can run etcd with the command `etcd` and you should see the
output contain the following lines:
```
   | etcd: listening for peers on http://localhost:2380
   | etcd: listening for peers on http://localhost:7001
   | etcd: listening for client requests on http://localhost:2379
   | etcd: listening for client requests on http://localhost:4001
```

Note that this will run an etcd server and create a new directory at that location
where it stores all of the records. This directory can be removed afterwards, or
you can simply run etcd in a temporary directory.

## Running the API Server

### Server Configuration

#### jwt token

To run the routing-api server, a configuration file with the public uaa jwt token must be provided.
This configuration file can then be passed in with the flag `-config [path_to_config]`.
An example of the configuration file can be found under `example_config/example.yml` for bosh-lite.

To generate your own config file, you must provide a `uaa_verification_key` in pem format, such as the following:

```
uaa_verification_key: "-----BEGIN PUBLIC KEY-----

      MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDHFr+KICms+tuT1OXJwhCUtR2d

      KVy7psa8xzElSyzqx7oJyfJ1JZyOzToj9T5SfTIq396agbHJWVfYphNahvZ/7uMX

      qHxf+ZH9BL1gk9Y6kCnbM5R60gfwjyW1/dQPjOzn9N394zd2FJoFHwdq9Qs0wBug

      spULZVNRxq7veq/fzwIDAQAB

      -----END PUBLIC KEY-----"
```

This can be found in your Cloud Foundry manifest under `uaa.jwt.verification_key`

#### Oauth Clients

The Routing API uses OAuth tokens to authenticate clients. To obtain a token from UAA that grants the API client permission to register routes, an OAuth client must first be created for the API client in UAA. An API client can then authenticate with UAA using the registered OAuth client credentials, request a token, then provide this token with requests to the Routing API.

Registering OAuth clients can be done using the cf-release BOSH deployment manifest, or manually using the `uaac` CLI for UAA.

- For API clients that wish to register/unregister routes with the Routing API, the OAuth client in UAA must be configured with the `routing.routes.write` authority.
- For API clients that wish to list routes with the Routing API, the OAuth client in UAA must be configured with the `routing.routes.read` authority.
- For API clients that wish to list router groups with the Routing API, the OAuth client in UAA must be configured with the `routing.router_groups.read` authority.

For instructions on fetching a token, see [Using the API manually](#authorization-token).

##### Configure OAuth clients in the cf-release BOSH Manifest

E.g:
```
uaa:
   clients:
      routing_api_client:
         authorities: routing.routes.write,routing.routes.read,routing.router_groups.read
         authorized_grant_type: client_credentials
         secret: route_secret
```

##### Configure OAuth clients manually using `uaac` CLI for UAA

1. Install the `uaac` CLI

   ```
   gem install cf-uaac
   ```

2. Get the admin client token

   ```bash
   uaac target uaa.bosh-lite.com
   uaac token client get admin # You will need to provide the client_secret, found in your CF manifest.
   ```

3. Create the OAuth client.

   ```bash
   uaac client add routing_api_client --authorities "routing.routes.write,routing.routes.read,routing.router_groups.read" --authorized_grant_type "client_credentials"
   ```

### Starting the Server

To run the API server you need to provide all the urls for the etcd cluster, a configuration file containg the public uaa jwt key, plus some optional flags.

Example 1:

```sh
routing-api -ip 127.0.0.1 -systemDomain 127.0.0.1.xip.io -config example_config/example.yml -port 3000 -maxTTL 60 http://etcd.127.0.0.1.xip.io:4001
```

Where `http://etcd.127.0.0.1.xip.io:4001` is the single etcd member.

Example 2:

```sh
routing-api http://etcd.127.0.0.1.xip.io:4001 http://etcd.127.0.0.1.xip.io:4002
```

Where `http://etcd.127.0.0.1.xip.io:4001` is one member of the cluster and `http://etcd.127.0.0.1.xip.io:4002` is another.

Note that flags have to come before the etcd addresses.

### Profiling the Server

The Routing API runs the [cf_debug_server](https://github.com/cloudfoundry-incubator/cf-debug-server), which is a wrapper around the go pprof tool. In order to generate this profile, do the following:

```bash
# Establish a SSH tunnel to your server (not necessary if you can connect directly)
ssh -L localhost:8080:[INTERNAL_SERVER_IP]:17002 vcap@[BOSH_DIRECTOR]
# Run the profile tool.
go tool pprof http://localhost:8080/debug/pprof/profile
```

## Using the API

The Routing API uses OAuth tokens to authenticate clients. To obtain a token from UAA an OAuth client must first be created for the API client in UAA. For instructions on registering OAuth clients, see [Server Configuration](#oauth-clients).

### Using the API with the `rtr` CLI

A CLI client called `rtr` has been created for the Routing API that simplifies interactions by abstracting authentication.

- [Documentation](https://github.com/cloudfoundry-incubator/routing-api-cli)
- [Downloads](https://github.com/cloudfoundry-incubator/routing-api-cli/releases)

### Using the API manually

#### Authorization Token

To obtain an token from UAA, use the `uaac` CLI for UAA.

1. Install the `uaac` CLI

   ```
   gem install cf-uaac
   ```

2. Retrieve the OAuth token using credentials for registered OAuth client

   ```bash
   uaac token client get routing_api_client
   ```

3. Display the `access_token`, which can be used as the Authorization header to `curl` the Routing API.

   ```
   uaac context
   ```

#### `curl` Examples

To add a route to the API server:

```sh
curl -vvv -H "Authorization: bearer [token with uaa routing.routes.write scope]" -X POST http://127.0.0.1:8080/routing/v1/routes -d '[{"ip":"1.2.3.4", "route":"a_route", "port":8089, "ttl":45}]'
```
To add a route, with an associated route service, to the API server. This must be a https-only url:

```sh
curl -vvv -H "Authorization: bearer [token with uaa routing.routes.write scope]" -X POST http://127.0.0.1:8080/routing/v1/routes -d '[{"ip":"1.2.3.4", "route":"a_route", "port":8089, "ttl":45, "route_service_url":"https://route-service.example.cf-app.com"}]'
```

To add a tcp route to the API server:

```sh
curl -vvv -H "Authorization: bearer [token with uaa routing.routes.write scope]" -X POST http://127.0.0.1:8080/routing/v1/tcp_routes/create -d '
[
  {
   "router_group_guid": "tcp-default",
   "port": 5200,
   "backend_ip": "10.1.1.12",
   "backend_port": 60000
   }
]'
```

To delete a route:

```sh
curl -vvv -H "Authorization: bearer [token with uaa routing.routes.write scope]" -X DELETE http://127.0.0.1:8080/routing/v1/routes -d '[{"ip":"1.2.3.4", "route":"a_route", "port":8089, "ttl":45}]'
```

To delete a tcp route:

```sh
curl -vvv -H "Authorization: bearer [token with uaa routing.routes.write scope]" -X POST http://127.0.0.1:8080/routing/v1/tcp_routes/delete -d '
[
  {
  "router_group_guid": "tcp-default",
  "port": 5200,
  "backend_ip": "10.1.1.12",
  "backend_port": 60000
  }
]'
```

To list registered routes:
```sh
curl -vvv -H "Authorization: bearer [token with uaa routing.routes.read scope]" http://127.0.0.1:8080/routing/v1/routes
```

To list registered tcp routes:
```sh
curl -vvv -H "Authorization: bearer [token with uaa routing.routes.read scope]" http://127.0.0.1:8080/routing/v1/tcp_routes

Sample response:
[
  {
    "router_group_guid": "tcp-default",
    "port": 5200,
    "backend_ip": "10.1.1.12",
    "backend_port": 60000
  }
]
```

To subscribe to route changes:
```sh
curl -vvv -H "Authorization: bearer [token with uaa routing.routes.read scope]" http://127.0.0.1:8080/routing/v1/events
```

To subscribe to tcp route changes:
```sh
curl -vvv -H "Authorization: bearer [token with uaa routing.routes.read scope]" http://127.0.0.1:8080/routing/v1/tcp_routes/events
```

To list available Router Groups:
```sh
curl -vvv -H "Authorization: bearer [token with uaa routing.router_groups.read scope]" http://127.0.0.1:8080/routing/v1/router_groups

Sample response:
[{
    "guid": "f7392031-a488-4890-8835-c4a038a3bded",
    "name": "default-tcp",
    "type": [
        "tcp"
    ]
}]
```

## Known issues

+ The routing-api will return a 404 if you attempt to hit the endpoint `http://[router host]/routing/v1/routes/` as opposed to `http://[router host]/routing/v1/routes`
+ The routing-api currently logs everything to the ctl log.
