# RSS CLI
Command line tool for reading and writing route service signatures.

## Building

```
cd ./test_util/rss
go build
```

## Using RSS cli

```
NAME:
   rss - A CLI for generating and opening a route service signature.

USAGE:
   rss [global options] command [command options] [arguments...]

VERSION:
   0.1.0

AUTHOR(S):
   Cloud Foundry Routing Team <cf-dev@lists.cloudfoundry.org>

COMMANDS:
   generate, g  Generates a Route Service Signature
   read, r, o   Decodes and decrypts a route service signature
   help, h      Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h           show help
   --version, -v        print the version
```


For example:

In the following example, we will generate a random key and then encrypt / decrypt signature.

- Generate key
      mkdir ~/.rss
      echo "my-super-secret-password" > ~/.rss/key

- Encrypt using key
      ./rss generate --url http://myapp.com

      Encoded Signature:
      AfirJQ7-m-AMByj7y5e4Z0U0_gi6EF29all4mlsyc94YbPu1OYBCL9cT0kyCTkOuOPAbfeZHUs6fHfgrPK54a6BmoKZOdSJO_YWU4F65ja2ZyXH36dlLAD3cHlh4KCyTdBwLQ88M8U39X2A=

      Encoded Metadata:
      eyJpdiI6Im1aN0JVY0NQVTVPazdFR1EiLCJub25jZSI6IldXSzNrYWIvVDJlK2w5aU4ifQ==

- Decrypt the signed header with key and metadata

      ./rss read --signature AfirJQ7-m-AMByj7y5e4Z0U0_gi6EF29all4mlsyc94YbPu1OYBCL9cT0kyCTkOuOPAbfeZHUs6fHfgrPK54a6BmoKZOdSJO_YWU4F65ja2ZyXH36dlLAD3cHlh4KCyTdBwLQ88M8U39X2A= --metadata eyJpdiI6Im1aN0JVY0NQVTVPazdFR1EiLCJub25jZSI6IldXSzNrYWIvVDJlK2w5aU4ifQ==

      Decoded Signature:
      {
        "forwarded_url": "http://myapp.com",
        "requested_time": "2015-08-13T11:16:48.081153827-07:00"
      }
