# qoru Design

## Overview

`qoru` is an experimental QUIC-based network relay/proxy. The current implementation supports a basic authenticated one-hop TCP proxy:

```text
TCP client -> qoru client -> QUIC/mTLS -> qoru server -> TCP target
```

The long-term direction is a chainable relay overlay with optional multi-hop forwarding and end-to-end payload encryption. The current code is intentionally smaller: static config, one or more configured direct upstream servers, TCP forwarding, QUIC transport, and mTLS authentication.

## Current Capabilities

Implemented today:

- Cobra CLI with `client`, `server`, and `print-config` commands.
- YAML config loading, default config path resolution, and validation.
- Development certificate generation.
- TLS 1.3 / mTLS identity loading from configured cert/key/CA files.
- QUIC transport using `quic-go`.
- Custom binary control protocol.
- Client-side local TCP listeners from `forwards`.
- One reconnecting upstream QUIC connection per configured client-side server.
- Multiple configured direct upstream servers selected by forward `egress`.
- On-demand upstream reconnect for new local TCP connections after connection loss.
- One QUIC stream per proxied local TCP connection.
- Multiple local TCP forwards.
- Server support for multiple streams per QUIC connection.
- Server-side named TCP services with per-service peer authorization.
- Optional one-hop egress selection; selected egress must currently be the connected server unless an explicit route is provided.
- Explicit-route multi-hop TCP forwarding through configured next-hop servers.
- Server-side TCP target dialing with timeout and basic service target address validation.
- `ConnectResponse` success/failure handshake before raw TCP proxying begins.
- Bidirectional byte proxying between local TCP, QUIC streams, and server-side TCP targets.

Not implemented yet:

- resuming active proxied TCP connections across upstream QUIC reconnects
- automatic multi-hop route selection
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

Loads and validates client config, establishes one QUIC/mTLS connection to each configured upstream qoru server, starts all configured local TCP listeners, and opens one QUIC stream per accepted local TCP connection. If an upstream QUIC connection later fails, the client keeps its local listeners open and reconnects that upstream on demand for future local TCP connections.

For each local TCP connection:

1. select an upstream by the forward's `egress` value, or the sole configured upstream when `egress` is empty
2. generate a UUIDv7 request ID
3. open a new QUIC stream on the selected upstream connection
4. send `ConnectRequest{RequestID: "...", Protocol: "tcp", Service: "...", Egress: "...", Route: [...]}`
5. read `ConnectResponse`
5. if OK, proxy bytes between the local TCP connection and QUIC stream

### `qoru server`

Loads and validates server config, loads its TLS identity, starts a QUIC listener, accepts QUIC connections, accepts multiple streams per connection, reads `ConnectRequest`, resolves the requested service, dials the service target, sends `ConnectResponse`, and proxies bytes between the QUIC stream and target TCP connection.

### `qoru print-config`

Loads config, validates it according to its `mode`, prints normalized YAML to stdout, then exits.

`print-config` writes directly to stdout and does not emit runtime logs.

## End-State Routing Model

The long-term routing model is service-first with optional client-side routing constraints.

A local forward primarily identifies the service the client wants:

```yaml
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: postgres-prod
```

In this mode, qoru may eventually choose an eligible egress node and route automatically. This enables service-level load balancing, failover, and topology-aware routing when multiple egress nodes can provide the same service.

A client may also constrain the egress node while still allowing qoru to choose the path to that egress:

```yaml
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: postgres-prod
    egress: relay-b
```

In this mode, `egress` means "the service must exit at this node." It is a routing constraint, not the service identity itself.

For multi-hop topologies, a client may eventually specify an explicit route:

```yaml
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: postgres-prod
    route:
      - relay-a
      - relay-b
```

If both `route` and `egress` are specified, the final route hop must match the requested egress:

```yaml
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: postgres-prod
    egress: relay-b
    route:
      - relay-a
      - relay-b
```

Conceptually:

- `service` is what the client wants.
- `egress` optionally constrains where traffic must exit.
- `route` optionally constrains the exact hop sequence.
- if neither `egress` nor `route` is set, qoru may choose automatically.
- if `route` is set, the last hop is the egress node.
- if both `route` and `egress` are set, they must agree.
- authorization policy can still reject any automatic or explicit routing decision.

This model supports both automatic routing and client-specified routing without making either one mandatory. The current implementation is intentionally narrower: service names are resolved on the selected direct upstream server, and with multiple configured upstream servers the client must set `egress` explicitly.

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

servers:
  - id: server-1
    address: 127.0.0.1:4433

forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: echo
    egress: server-1
```

The client requests a named service. Service names are currently resolved on the selected direct upstream server. `egress` is optional when exactly one upstream server is configured; empty means that server may satisfy the request. When multiple upstream servers are configured, each forward must set `egress` to one configured server ID. In the current one-hop implementation, the selected server also requires any non-empty request `egress` to match its own `node_id`.

A forward may include a `route` field for explicit multi-hop routing. The first hop must be a configured direct upstream server. The final hop is the egress node:

```yaml
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: echo
    egress: server-1
    route:
      - relay-a
      - relay-b
```

When a relay receives a routed request, the route is interpreted as the remaining path beginning with the current node. The relay validates `route[0] == node_id`; if more hops remain, it dials `route[1]`, forwards the request with the current hop removed, and proxies stream bytes. The first multi-hop implementation uses hop-by-hop QUIC/mTLS only; intermediary relays can observe raw proxied bytes until end-to-end payload encryption is added.

A client may configure multiple direct upstream servers:

```yaml
servers:
  - id: server-1
    address: 127.0.0.1:4433
  - id: server-2
    address: 127.0.0.1:4434

forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: echo
    egress: server-1
  - protocol: tcp
    listen: 127.0.0.1:15433
    service: echo
    egress: server-2
```

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
- at least one `servers` entry
- each `servers[]` entry requires `id` and `address`
- at least one `forwards` entry
- each forward requires `protocol: tcp`, `listen`, and `service`; `egress` is optional with one upstream server and required with multiple upstream servers
- `route` is optional; when set, it must be non-empty and contain no empty hops
- route length is capped at `3` hops for the first multi-hop implementation
- the first route hop must match a configured direct upstream server
- if both `route` and `egress` are set, `egress` must match the final route hop

Server required fields:

- `mode: server`
- `listen`

Server service fields:

- `services`: named protocol-aware services this server can provide. Each service has `name`, `protocol`, `target`, and optional `peers`. If `peers` is omitted or empty, any authenticated peer may use that service.
- `peers`: optional configured relay peers. Each peer has `id`, optional `address`, and optional `dial`. Conceptually, `peers` represents allowed overlay neighbors. `dial: true` means this node should initiate/maintain an outbound connection to that peer; `address` is required when `dial: true`. `dial: false` or omitted means the peer relationship may be inbound-only. Current forwarding can use configured outbound peers; inbound-only peer registration is not implemented yet.

## TLS and Identity

qoru uses mTLS for peer authentication.

TLS config lives in `internal/identity`.

Current behavior:

- TLS 1.3 minimum
- private CA loaded from configured `identity.ca`
- system trust store is not used
- server requires and verifies client certificates
- client verifies the server certificate chain and qoru node URI SAN against the selected upstream server identity
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

The ingress client generates a UUIDv7 `request_id` once per local TCP connection. Relays forward this ID unchanged so logs can be correlated across all hops handling the same logical request.

Payload format:

```text
request_id   [16]byte // UUIDv7
protocol_len uint8
protocol     []byte
service_len  uint16 big endian
service      []byte
egress_len   uint16 big endian
egress       []byte
route_count  uint8
repeated route_count times:
  hop_len    uint16 big endian
  hop        []byte
```

`route` carries the remaining explicit path beginning with the node currently receiving the request. A relay forwards with the current hop removed, so `[relay-a, relay-b]` becomes `[relay-b]` when `relay-a` forwards to `relay-b`.

### `ConnectResponse`

Sent by the server after attempting to open the requested service.

Payload format:

```text
status      uint8   // 0 = OK, 1 = error
code        uint8   // machine-readable response code
message_len uint16 big endian
message     []byte
```

Current response codes:

```text
0 OK
1 SERVICE_NOT_FOUND
2 ACCESS_DENIED
3 TARGET_DIAL_FAILED
4 UNSUPPORTED_PROTOCOL
5 UNREACHABLE_EGRESS
6 ROUTE_INVALID
7 NEXT_HOP_UNREACHABLE
8 INTERNAL_ERROR
```

If status is OK, code must be `OK` and both sides hand the stream over to raw TCP proxying. If status is error, code identifies the failure class and message provides human-readable detail.

Current TCP stream model:

```text
[ConnectRequest frame]
[ConnectResponse frame]
[raw TCP bytes...]
```

The setup/control phase is framed. Once the server confirms success, the remaining stream bytes are proxied directly between the local TCP connection and target TCP connection.

Future multi-hop/end-to-end encryption will likely require framed encrypted data messages, but the current one-hop TCP implementation keeps raw bytes after the initial setup handshake.

## Timeouts and Reconnect Backoff

Timeouts and reconnect policy are currently hardcoded.

- client QUIC dial timeout: `10s`
- server TCP target dial timeout: `10s`
- server QUIC accept failure retry backoff: starts at `100ms`, doubles on consecutive failures, capped at `30s`, and resets after a successful accept
- client upstream reconnect backoff after failed dial attempts: `500ms`, `1s`, `2s`, `4s`, `8s`, `16s`, capped at `16s`
- server relay peer reconnect backoff for `dial: true` peers: `500ms`, `1s`, `2s`, `4s`, `8s`, `16s`, then `16s` forever until reconnect succeeds

Server service target dialing uses `net.Dialer.DialContext` and validates configured service targets with `net.SplitHostPort` before dialing. DNS lookup and dial errors are reported through `ConnectResponse`.

The server listener accept loop is resilient to transient QUIC accept failures. If accepting a connection fails while the server context is still active, qoru logs the failure, waits with exponential backoff, and retries instead of immediately shutting down. The backoff resets after a successful accept.

Client reconnect is on demand. qoru does not run a background reconnect loop and does not sleep inside local TCP handlers during backoff. If a local TCP connection arrives while the selected upstream is still in reconnect backoff, stream setup fails fast and the local TCP connection is closed without payload injection.

Relay peer reconnect for `dial: true` peers runs in the background and also reconnects on demand when a forwarded request needs a peer session. During peer reconnect backoff, forwarded requests fail fast with `NEXT_HOP_UNREACHABLE`.

Timeouts and reconnect policy are not yet configurable.

## Logging

Request-scoped logs include `request_id`, a UUIDv7 generated by the ingress client and forwarded unchanged through explicit-route relays.

Runtime commands use Go's standard `log/slog` text handler, which emits logfmt-style output:

```text
time=... level=INFO msg="server listening" node_id=server-1 addr=127.0.0.1:4433
```

Runtime logs go to stderr so stdout can remain available for command output.

Client reconnect observability is split between upstream-session events and local TCP setup events:

- upstream reconnect attempts after previous failures are logged at `Info`
- failed upstream reconnect dials are logged at `Warn` with `server_id`, `addr`, `backoff`, and `next_attempt`
- successful reconnects after previous failures are logged at `Info`
- service/policy rejections are logged at `Warn`
- reconnect-backoff local connection failures are logged at `Warn` with `server_id`, `addr`, and `next_attempt`
- other stream setup or transport failures are logged at `Error`

`qoru print-config` writes YAML directly to stdout and does not initialize runtime logging.

## Client Runtime

The current client runtime lives in `internal/client`.

`client.Run` currently:

1. validates client config
2. establishes one QUIC/mTLS connection to each configured upstream server
3. binds one local TCP listener per configured `forwards` entry
4. starts accept loops for all listeners
5. for each local TCP connection, selects an upstream session by forward `egress` and opens a new QUIC stream
6. sends `ConnectRequest{RequestID: "...", Protocol: "tcp", Service: "...", Egress: "...", Route: [...]}`
7. waits for `ConnectResponse`
8. proxies bytes between the local TCP connection and QUIC stream
9. exits cleanly when the context is canceled
10. keeps local listeners running if the upstream QUIC connection later fails
11. reconnects the selected upstream on demand when a later local TCP connection needs a new stream

Relevant client package files:

- `client.go` contains runtime orchestration, local TCP listeners, and local connection handling.
- `session.go` contains upstream session selection and reconnect management.
- `stream.go` contains QUIC dialing and the `ConnectRequest`/`ConnectResponse` stream setup handshake.
- `proxy.go` contains byte proxying between local TCP connections and QUIC streams.

Current limitation: reconnect is on demand and applies only to future local TCP connections. Active proxied TCP connections are bound to streams on the old QUIC connection; if that connection dies, those TCP connections are closed rather than resumed. Failed reconnect dials use exponential backoff capped at `16s`; successful reconnect resets the backoff state.

## Peer Session Direction Model

The long-term relay model treats node-to-node QUIC connections as logical peer sessions, independent of which side initiated the underlying connection.

Peer config should be understood as an allowed overlay-neighbor relationship, not merely a dial target. In the current implementation, only the side that initiates a relay-to-relay connection needs a `peers` entry with `address` and `dial: true`. In the target model, both sides should usually define each other as peers; `dial` controls whether a node initiates the connection or only accepts/registers an inbound session.

A relay/server should eventually:

1. start its QUIC listener
2. initiate outbound QUIC connections to configured peers at startup
3. accept inbound QUIC connections from peers
4. authenticate all peers with mTLS node identity
5. register each connection as a session keyed by remote node ID
6. use the session manager for forwarding to a next hop

In this model, route forwarding asks for an authenticated peer session by node ID rather than dialing the next hop ad hoc for every proxied TCP connection.

For now, do not configure both sides of a peer relationship with `dial: true`. Exactly one side should initiate and the other side should list the peer without `dial` or with `dial: false`. Mutual dialing is intentionally unsupported for now to keep session ownership simple.

Current implementation note: explicit-route relay forwarding uses peer sessions keyed by authenticated node ID. A server attempts to dial configured `dial: true` peers at startup, but startup continues if a peer is unavailable. Dialing peers are maintained by background reconnect loops with capped exponential backoff: `500ms`, `1s`, `2s`, `4s`, `8s`, `16s`, then `16s` forever until reconnect succeeds. Forwarding also reconnects on demand when backoff allows; during backoff, route requests fail fast with `NEXT_HOP_UNREACHABLE`. Accepted inbound connections from configured peers are also registered as reusable peer sessions, so either side can open streams on the same QUIC connection. Inbound connections from nodes not listed in `peers` are still accepted for normal client/service use, but are not registered as relay peer sessions. If a duplicate peer session appears, the first live session wins and the duplicate is logged and closed.

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

For routed requests, the receiving server validates that the first remaining route hop is its own `node_id`. If additional hops remain, it dials the next configured peer, forwards the request with its own hop removed from `route`, relays the downstream `ConnectResponse` back upstream, and then proxies raw bytes between QUIC streams.

Current limitation: accepted inbound peer connections are registered for forwarding when the authenticated node ID appears in `peers`, but mutual dialing is unsupported. If a duplicate peer session appears, the first live session wins and the duplicate is logged and closed.

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
relay-a.crt
relay-a.key
relay-b.crt
relay-b.key
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

Automated smoke tests are available:

```sh
make demo-e2e
make demo-multihop
make demo-threehop
```

`demo-multihop` exercises:

```text
local TCP client -> qoru client -> relay-a -> relay-b -> echo target
```

`demo-threehop` exercises the current maximum explicit route length:

```text
local TCP client -> qoru client -> relay-a -> relay-b -> relay-c -> echo target
```

See `docs/local-demo.md` for details.

## Current Package Layout

```text
cmd/qoru/              CLI entrypoint
internal/cli/          Cobra commands and command wiring
internal/client/       QUIC client runtime, upstream sessions, stream setup, and local TCP proxying
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
3. Improve reconnect observability and clearer server-side session handling.
4. Consider configurable log level/log format and timeout settings.
5. Improve duplicate peer-session diagnostics and validation where possible.
6. Improve explicit-route multi-hop smoke tests and demo docs.
7. Add richer service selection semantics for future multi-egress/load-balanced service routing.
8. Later: end-to-end encrypted payload frames.
