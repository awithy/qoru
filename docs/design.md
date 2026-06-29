# qoru Design

## Overview

`qoru` is an experimental QUIC-based network relay/proxy. The current implementation supports a basic authenticated one-hop TCP proxy:

```text
TCP client -> qoru client -> QUIC/mTLS -> qoru server -> TCP target
```

The long-term direction is a chainable relay overlay with optional multi-hop forwarding and end-to-end payload encryption. The current code is intentionally smaller: static config, one configured server, TCP forwarding, QUIC transport, and mTLS authentication.

## Current Capabilities

Implemented today:

- Cobra CLI with `client`, `server`, and `print-config` commands.
- YAML config loading, default config path resolution, and validation.
- Development certificate generation.
- TLS 1.3 / mTLS identity loading from configured cert/key/CA files.
- QUIC transport using `quic-go`.
- Custom binary control protocol.
- Client-side local TCP listeners from `forwards`.
- One shared QUIC connection from client to server.
- One QUIC stream per proxied local TCP connection.
- Multiple local TCP forwards.
- Server support for multiple streams per QUIC connection.
- Server-side named TCP services with per-service peer authorization.
- Optional one-hop egress selection; selected egress must currently be the connected server.
- Server-side TCP target dialing with timeout and basic service target address validation.
- `ConnectResponse` success/failure handshake before raw TCP proxying begins.
- Bidirectional byte proxying between local TCP, QUIC streams, and server-side TCP targets.

Not implemented yet:

- reconnect behavior if the shared QUIC connection dies
- multi-hop forwarding
- end-to-end encrypted payload frames
- UDP forwarding

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

Loads and validates client config, establishes one QUIC/mTLS connection to the configured qoru server, starts all configured local TCP listeners, and opens one QUIC stream per accepted local TCP connection.

For each local TCP connection:

1. open a new QUIC stream on the shared QUIC connection
2. send `ConnectRequest{Protocol: "tcp", Service: "...", Egress: "..."}`
3. read `ConnectResponse`
4. if OK, proxy bytes between the local TCP connection and QUIC stream

### `qoru server`

Loads and validates server config, loads its TLS identity, starts a QUIC listener, accepts QUIC connections, accepts multiple streams per connection, reads `ConnectRequest`, resolves the requested service, dials the service target, sends `ConnectResponse`, and proxies bytes between the QUIC stream and target TCP connection.

### `qoru print-config`

Loads config, validates it according to its `mode`, prints normalized YAML to stdout, then exits.

`print-config` writes directly to stdout and does not emit runtime logs.

## Configuration

The current config format is YAML.

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

forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: echo
    egress: server-1
```

The client requests a named service. `egress` is optional today; if set in the current one-hop implementation, it must match the connected server's `node_id`.

### Server config

```yaml
node_id: server-1
mode: server

identity:
  cert: ./dev/certs/server-1.crt
  key: ./dev/certs/server-1.key
  ca: ./dev/certs/ca.crt

listen: 127.0.0.1:4433

services:
  - name: echo
    protocol: tcp
    target: 127.0.0.1:9000
    peers:
      - client-1
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
- at least one `forwards` entry
- each forward requires `protocol: tcp`, `listen`, and `service`; `egress` is optional

Server required fields:

- `mode: server`
- `listen`

Server service fields:

- `services`: named protocol-aware services this server can provide. Each service has `name`, `protocol`, `target`, and optional `peers`. If `peers` is omitted or empty, any authenticated peer may use that service.

## TLS and Identity

qoru uses mTLS for peer authentication.

TLS config lives in `internal/identity`.

Current behavior:

- TLS 1.3 minimum
- private CA loaded from configured `identity.ca`
- system trust store is not used
- server requires and verifies client certificates
- client verifies the server certificate chain and qoru node URI SAN against the configured server identity
- ALPN is set to `qoru/1`

The current identity model requires SPIFFE-style URI SAN identities:

```text
URI:spiffe://qoru/node/client-1
URI:spiffe://qoru/node/server-1
```

The node ID is extracted from the URI SAN and used as the authenticated peer identity. DNS SANs and certificate Common Names are not used for qoru node identity.

## ALPN

ALPN is Application-Layer Protocol Negotiation. qoru currently advertises:

```text
qoru/1
```

This identifies the application protocol being spoken over QUIC/TLS and gives us a future versioning point.

## Binary Protocol

qoru uses a small custom binary protocol for stream setup/control messages. It intentionally does not use JSON or protobuf.

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
TypeConnectRequest = 1
TypeConnectResponse = 2
MaxPayloadSize = 64*1024 - 1
MaxProtocolLength = 32
MaxTargetLength = 4096
```

### `ConnectRequest`

Sent by the client to ask the server to open a named service for a protocol. Currently only `protocol: tcp` is supported at runtime.

Payload format:

```text
protocol_len uint8
protocol     []byte
service_len  uint16 big endian
service      []byte
egress_len   uint16 big endian
egress       []byte
```

### `ConnectResponse`

Sent by the server after attempting to open the requested service.

Payload format:

```text
status      uint8   // 0 = OK, 1 = error
message_len uint16 big endian
message     []byte
```

If status is OK, both sides hand the stream over to raw TCP proxying.

Current TCP stream model:

```text
[ConnectRequest frame]
[ConnectResponse frame]
[raw TCP bytes...]
```

The setup/control phase is framed. Once the server confirms success, the remaining stream bytes are proxied directly between the local TCP connection and target TCP connection.

Future multi-hop/end-to-end encryption will likely require framed encrypted data messages, but the current one-hop TCP implementation keeps raw bytes after the initial setup handshake.

## Timeouts

Timeouts are currently hardcoded.

- client QUIC dial timeout: `10s`
- server TCP target dial timeout: `10s`

Server service target dialing uses `net.Dialer.DialContext` and validates configured service targets with `net.SplitHostPort` before dialing. DNS lookup and dial errors are reported through `ConnectResponse`.

Timeouts are not yet configurable.

## Logging

Runtime commands use Go's standard `log/slog` text handler, which emits logfmt-style output:

```text
time=... level=INFO msg="server listening" node_id=server-1 addr=127.0.0.1:4433
```

Runtime logs currently go to stdout.

`qoru print-config` writes YAML directly to stdout and does not initialize runtime logging.

## Client Runtime

The current client runtime lives in `internal/client`.

`client.Run` currently:

1. validates client config
2. establishes one QUIC/mTLS connection to the configured server
3. binds one local TCP listener per configured `forwards` entry
4. starts accept loops for all listeners
5. for each local TCP connection, opens a new QUIC stream on the shared connection
6. sends `ConnectRequest{Protocol: "tcp", Service: "...", Egress: "..."}`
7. waits for `ConnectResponse`
8. proxies bytes between the local TCP connection and QUIC stream
9. exits cleanly when the context is canceled
10. returns an error if the shared QUIC connection closes unexpectedly

Useful lower-level helpers:

- `Connect` opens the shared QUIC connection.
- `OpenTCPStream` opens a stream and performs the `ConnectRequest`/`ConnectResponse` handshake.
- `ConnectTCP` opens a fresh QUIC connection and stream; this is primarily useful in tests and small helper flows.

Current limitation: no reconnect behavior yet. If the shared QUIC connection dies, the client runtime exits instead of reconnecting.

## Server Runtime

The current server runtime lives in `internal/server`.

`server.Run` currently:

1. validates server config
2. loads server mTLS config
3. starts a QUIC listener on `cfg.Listen`
4. logs the bound address
5. accepts QUIC connections
6. accepts multiple streams per QUIC connection
7. reads `ConnectRequest` per stream
8. validates that the requested protocol is currently supported (`tcp`)
9. validates optional `egress` against this server's `node_id`
10. resolves and authorizes the requested service for the authenticated peer
11. dials the configured TCP service target with timeout
12. sends `ConnectResponse`
13. if OK, proxies bytes between the QUIC stream and TCP target
14. exits cleanly when the context is canceled

Current limitation: service requests are one-hop only. If `egress` is set, it must match the connected server's `node_id`; multi-hop routing to another egress is not implemented yet.

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

## Local Demo

A local echo-server demo is available:

```sh
make gen-dev-certs
go run ./dev/echo-server -listen 127.0.0.1:9000
go run ./cmd/qoru server -c examples/config/server.yaml
go run ./cmd/qoru client -c examples/config/client.yaml
nc 127.0.0.1 15432
```

See `docs/local-demo.md` for details.

## Current Package Layout

```text
cmd/qoru/              CLI entrypoint
internal/cli/          Cobra commands and command wiring
internal/client/       QUIC client runtime and local TCP proxying
internal/config/       config structs, path resolution, YAML load/marshal, validation
internal/identity/     TLS and mTLS identity loading
internal/protocol/     custom binary frame protocol
internal/server/       QUIC server runtime and TCP proxying
dev/echo-server/       local TCP echo target for demos
dev/                   local development helpers
examples/config/       example client/server YAML configs
docs/                  design documentation
```

## Near-Term Next Steps

1. Improve active connection shutdown and goroutine lifecycle tracking.
2. Add clearer local TCP behavior when service setup/dialing fails.
3. Add reconnect behavior for the shared client QUIC connection.
4. Consider configurable log level/log format and timeout settings.
5. Add richer service selection semantics for future multi-egress/load-balanced service routing.
6. Later: multi-hop forwarding and end-to-end encrypted payload frames.
