# qoru Design

## Overview

`qoru` is an experimental QUIC-based network relay/proxy. The first implementation slice targets a basic authenticated TCP proxy shape:

```text
TCP client -> qoru client -> QUIC/mTLS -> qoru server -> TCP target
```

The current codebase is scaffolding toward that goal. It includes CLI/config handling, development certificate generation, TLS identity loading, and a minimal QUIC server listener.

## CLI Shape

The CLI uses Cobra and has three top-level commands:

```sh
qoru client
qoru server
qoru print-config
```

There is intentionally no `run` subcommand. Running is implicit in the `client` and `server` commands.

All commands share a persistent config flag:

```sh
-c, --config string   path to qoru config file
```

If `--config` is omitted, qoru resolves config using the first existing path from:

```text
./qoru.yaml
./qoru.yml
/etc/qoru/config.yaml
/etc/qoru/config.yml
```

### `qoru client`

Loads and validates a client config. The client runtime is still a placeholder.

### `qoru server`

Loads and validates a server config, loads its TLS identity, starts a QUIC listener, and blocks until canceled.

### `qoru print-config`

Loads config, validates it according to its `mode`, prints normalized YAML to stdout, then exits.

`print-config` writes directly to stdout and does not emit runtime logs.

## Configuration

The first-slice config format is YAML.

### Client config

```yaml
node_id: client-1
mode: client

identity:
  cert: ./dev/certs/client-1.crt
  key: ./dev/certs/client-1.key
  ca: ./dev/certs/ca.crt

server:
  id: server-1
  address: 127.0.0.1:4433

tcp_forwards:
  - listen: 127.0.0.1:15432
    target: 127.0.0.1:5432
```

For the first TCP proxy slice, the client is allowed to request any target. Server-side access policy is deferred to a future slice.

### Server config

```yaml
node_id: server-1
mode: server

identity:
  cert: ./dev/certs/server-1.crt
  key: ./dev/certs/server-1.key
  ca: ./dev/certs/ca.crt

listen: 127.0.0.1:4433
```

## Config Validation

Shared required fields:

- `node_id`
- `mode`
- `identity.cert`
- `identity.key`
- `identity.ca`

Client required fields:

- `mode: client`
- `server.id`
- `server.address`
- at least one `tcp_forwards` entry
- each forward requires `listen` and `target`

Server required fields:

- `mode: server`
- `listen`

## TLS and Identity

qoru uses mTLS for peer authentication.

TLS config lives in `internal/identity`.

Current behavior:

- TLS 1.3 minimum
- private CA loaded from configured `identity.ca`
- system trust store is not used
- server requires and verifies client certificates
- client verifies the server certificate against the configured server identity
- ALPN is set to `qoru/1`

The current identity model uses certificate SANs. For development, certificates include DNS SANs such as:

```text
DNS:client-1
DNS:server-1
IP:127.0.0.1
```

No real DNS lookup is required for `server-1`; the name is used as the expected certificate identity.

Longer term, qoru may move to URI SAN identities such as:

```text
spiffe://qoru/node/server-1
```

## ALPN

ALPN is Application-Layer Protocol Negotiation. qoru currently advertises:

```text
qoru/1
```

This identifies the application protocol being spoken over QUIC/TLS and gives us a future versioning point.

## Logging

Runtime commands use Go's standard `log/slog` text handler, which emits logfmt-style output:

```text
time=... level=INFO msg="server listening" node_id=server-1 addr=127.0.0.1:4433
```

Runtime logs currently go to stdout.

`qoru print-config` writes YAML directly to stdout and does not initialize runtime logging.

## Server Runtime

The current server runtime lives in `internal/server`.

`server.Run` currently:

1. validates server config
2. loads server mTLS config
3. starts a QUIC listener on `cfg.Listen`
4. logs the bound address
5. accepts QUIC connections
6. immediately closes accepted connections with `not implemented`
7. exits cleanly when the context is canceled

No stream protocol or TCP proxying is implemented yet.

## CLI Runtime Wiring

The CLI uses function injection for runtime commands:

```go
type runnerFunc func(context.Context, *config.Config, *slog.Logger) error
```

This lets CLI tests verify that commands load config and call the expected runner without starting real QUIC listeners.

The real server runner delegates to `server.Run`.

## Development Certificates

Development certs are generated locally and not committed.

```sh
make gen-dev-certs
```

This writes files to:

```text
dev/certs/
```

The generated files include:

```text
ca.crt
ca.key
client-1.crt
client-1.key
server-1.crt
server-1.key
```

`dev/certs/` is ignored by git.

## Current Package Layout

```text
cmd/qoru/              CLI entrypoint
internal/cli/          Cobra commands and command wiring
internal/config/       config structs, path resolution, YAML load/marshal, validation
internal/identity/     TLS and mTLS identity loading
internal/server/       minimal QUIC server runtime
dev/                   local development helpers
examples/config/       example client/server YAML configs
docs/                  design documentation
```

## Near-Term Next Steps

1. Add minimal client runtime that connects to the QUIC server with mTLS.
2. Add an integration test that starts the server on `127.0.0.1:0` and connects with the client.
3. Define the first stream metadata frame containing the requested TCP target.
4. Implement server-side TCP dialing.
5. Proxy bytes between local TCP connection, QUIC stream, and target TCP connection.
6. Add server-side target access policy in a later slice.
