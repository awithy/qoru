# qoru Design

## Overview

`qoru` is an experimental QUIC-based network relay/proxy. The first implementation target is a basic authenticated TCP proxy shape:

```text
TCP client -> qoru client -> QUIC/mTLS -> qoru server -> TCP target
```

The current codebase has CLI/config handling, development certificate generation, TLS identity loading, a custom binary protocol, a QUIC server, a simple QUIC client, and server-side TCP target proxying over a QUIC stream.

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

Loads and validates a client config, connects to the configured qoru server with QUIC/mTLS, opens a QUIC stream, sends a `ConnectTCPRequest` for the first configured TCP forward target, then closes the stream.

The long-running local TCP listener is not implemented yet.

### `qoru server`

Loads and validates a server config, loads its TLS identity, starts a QUIC listener, accepts connections/streams, reads `ConnectTCPRequest`, dials the requested TCP target, and proxies bytes between the QUIC stream and target TCP connection.

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

## Binary Protocol

qoru uses a small custom binary protocol for control messages. It intentionally does not use JSON or protobuf.

Frame envelope:

```text
version uint8
type    uint8
length  uint16 big endian
payload []byte
```

Current constants:

```go
Version = 1
TypeConnectTCP = 1
MaxPayloadSize = 64*1024 - 1
MaxTargetLength = 4096
```

The first message is `ConnectTCPRequest`, meaning: ask the server to dial a TCP target.

Payload format:

```text
target_len uint16 big endian
target     []byte
```

Current TCP stream model:

```text
[ConnectTCPRequest frame][raw TCP bytes...]
```

The control request is framed. Once the server dials the TCP target, the remaining stream bytes are proxied directly between the QUIC stream and TCP connection.

Future multi-hop/end-to-end encryption will likely require framed encrypted data messages, but the first one-hop TCP slice keeps raw bytes after the initial control frame.

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
6. accepts one stream per connection currently
7. reads a `ConnectTCPRequest`
8. dials the requested TCP target
9. proxies bytes between the QUIC stream and TCP target
10. exits cleanly when the context is canceled

Current limitation: connection/stream lifecycle is still minimal. The server handles one stream per accepted connection in the current path.

## Client Runtime

The current client runtime lives in `internal/client`.

`client.Run` currently:

1. validates client config
2. connects to the configured qoru server with QUIC/mTLS
3. opens a stream
4. sends a `ConnectTCPRequest` for the first configured `tcp_forwards` target
5. closes the stream

`client.ConnectTCP` is a lower-level helper used by tests to open a QUIC stream to a target and leave it available for byte exchange.

Current limitation: the client does not yet start local TCP listeners from `tcp_forwards.listen`.

## CLI Runtime Wiring

The CLI uses function injection for runtime commands:

```go
type runnerFunc func(context.Context, *config.Config, *slog.Logger) error
```

This lets CLI tests verify that commands load config and call the expected runner without starting real QUIC listeners.

The real runners delegate to `client.Run` and `server.Run`.

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
internal/client/       QUIC client runtime and ConnectTCP helper
internal/config/       config structs, path resolution, YAML load/marshal, validation
internal/identity/     TLS and mTLS identity loading
internal/protocol/     custom binary frame protocol
internal/server/       QUIC server runtime and TCP proxying
dev/                   local development helpers
examples/config/       example client/server YAML configs
docs/                  design documentation
```

## Near-Term Next Steps

1. Implement qoru client local TCP listeners from `tcp_forwards.listen`.
2. For each local TCP connection, open a QUIC stream, send `ConnectTCPRequest`, and proxy bytes both ways.
3. Decide whether to reuse one QUIC connection for multiple streams or initially dial per accepted local connection.
4. Improve server handling to support multiple streams per QUIC connection.
5. Add full end-to-end integration test: local TCP client -> qoru client listener -> qoru server -> TCP echo target.
6. Add server-side target access policy in a later slice.
