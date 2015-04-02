# CF Routing API

## Installing this Repo

To clone this repo you will need to have godeps installed. You can install godeps
by running the command `go get github.com/tools/godep`.

To then install this repo you can run the following commands.

```sh
go get github.com/cloudfoundry-incubator/routing-api
cd $GOPATH/src/github.com/cloudfoundry-incubator/routing-api
godep restore
```

To install the server binary you can do

```sh
cd $GOPATH/src/github.com/cloudfoundry-incubator/routing-api
go install ./cmd/routing-api

# OR
go get github.com/cloudfoundry-incubator/routing-api/cmd/routing-api
```

## Development

#### etcd

To run the tests you need a running etcd cluster on version 0.4.6. To get that do:

```sh
go get github.com/coreos/etcd
cd $GOPATH/src/github.com/coreos/etcd
git fetch --tags
git checkout v0.4.6
go install .
```

Once installed, you can run etcd with the command `etcd` and you should see the
following output:
```
   | Using the directory majestic.etcd as the etcd curation directory because a directory was not specified.
   | majestic is starting a new cluster
   | etcd server [name majestic, listen on :4001, advertised url http://127.0.0.1:4001]   <-- default location of the etcd server
   | peer server [name majestic, listen on :7001, advertised url http://127.0.0.1:7001]
```

Note that this will run an etcd server and create a new directory at that location 
where it stores all of the records. This directory can be removed afterwards, or 
you can simply run etcd in a temporary directory.

#### Authorization Token

To easily generate a token with the `route.advertise` scope, you will need to
install the `uaac` CLI tool (`gem install cf-uaac`) and follow these steps:

```bash
uaac target uaa.10.244.0.34.xip.io
uaac token client get admin # You will need to provide the client_secret, found in your CF manifest.
uaac client add route --authorities "route.advertise" --authorized_grant_type "client_credentials"
uaac token client get route
uaac context
```

The last command will show you the client's token, which can then be used to
curl the Routing API as a Authorization header.

## API Server Configuration

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

## UAA Scope

To interact with the Routing API server, you must provide a authorization token
with the `route.advertise` scope enabled. Any client that wishes to register
routes with the Routing API should have the `authorities: route.advertise`
specified in the CF manifest.

E.g:
```
uaa:
   clients:
      route_advertise_client:
         authorities: route.advertise
         authorized_grant_type: client_credentials
         secret: route_secret
```

## Usage

To run the API server you need to provide all the urls for the etcd cluster, a configuration file containg the public uaa jwt key, plus some optional flags.

Example 1:

```sh
routing-api -config example_config/example.yml -port 3000 -maxTTL 60 http://etcd.127.0.0.1.xip.io:4001
```

Where `http://etcd.127.0.0.1.xip.io:4001` is the single etcd member.

Example 2:

```sh
routing-api http://etcd.127.0.0.1.xip.io:4001 http://etcd.127.0.0.1.xip.io:4002
```

Where `http://etcd.127.0.0.1.xip.io:4001` is one member of the cluster and `http://etcd.127.0.0.1.xip.io:4002` is another.

Note that flags have to come before the etcd addresses.

To add a route to the API server:

```sh
curl -vvv -H "Authorization: bearer [token with uaa route.advertise scope]" -X POST http://127.0.0.1:8080/v1/routes -d '[{"ip":"1.2.3.4", "route":"a_route", "port":8089, "ttl":45}]'
```

To delete a route:

```sh
curl -vvv -H "Authorization: bearer [token with uaa route.advertise scope]" -X DELETE http://127.0.0.1:8080/v1/routes -d '[{"ip":"1.2.3.4", "route":"a_route", "port":8089, "ttl":45}]'
```

## Known issues

+ The routing-api will return a 404 if you attempt to hit the endpoint `http://[router host]/v1/routes/` as opposed to `http://[router host]/v1/routes`
+ The routing-api currently logs everything to the ctl log.
