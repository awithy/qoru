# qoru Handoff Memo

## Current State

`qoru` is an experimental QUIC-based TCP relay/proxy written in Go. It has moved beyond the original one-hop proxy and now supports explicit-route multi-hop TCP forwarding over authenticated QUIC/mTLS.

Supported topologies today:

```text
TCP client -> qoru client -> qoru server -> TCP target
TCP client -> qoru client -> relay-a -> relay-b -> TCP target
TCP client -> qoru client -> relay-a -> relay-b -> relay-c -> TCP target
```

The current implementation is still experimental. APIs, config, and protocol details are expected to change.

## Major Implemented Capabilities

- Cobra CLI:
  - `qoru client`
  - `qoru server`
  - `qoru print-config`
- YAML config.
- QUIC transport using `quic-go`.
- mTLS peer authentication using configured private CA.
- SPIFFE-style URI SAN node identities:
  - `spiffe://qoru/node/<node-id>`
- Custom binary control protocol.
- Machine-readable `ConnectResponse` codes.
- One QUIC stream per proxied TCP connection.
- Client-side local TCP forwards.
- Multiple client upstream entry points via `servers`.
- Server/relay peer config via `peers`.
- Explicit-route multi-hop forwarding.
- Startup dialing for configured relay peers with `dial: true`.
- Inbound peer session registration for configured peers.
- Reusable peer QUIC connections.
- Server-side named TCP services.
- Per-service peer authorization.
- One-hop, two-hop, and three-hop smoke demos.

## Important Current Limitations

- Multi-hop payloads are **not end-to-end encrypted** yet.
  - Intermediary relays currently proxy raw TCP bytes and can observe payload.
- Only explicit routes are supported.
  - No automatic route selection.
  - No load balancing.
  - No service discovery.
- Route length is currently capped at `3` hops.
- UDP forwarding is not implemented.
- Active proxied TCP streams are not resumed after connection loss.
- Mutual peer dialing is unsupported.
  - Configure `dial: true` on only one side of a peer relationship.
  - Duplicate peer sessions are logged and closed; first live session wins.
- Peer reconnect behavior is still basic.
  - Startup `dial: true` peer failures currently fail server startup.
- Logs do not yet include request IDs / trace IDs.
- No topology/status CLI yet.

## Config Model

### Client Mode

Client configs use `servers` for direct upstream entry points.

```yaml
node_id: client-1
mode: client

identity:
  cert: ./dev/certs/client-1.crt
  key: ./dev/certs/client-1.key
  ca: ./dev/certs/ca.crt

servers:
  - id: relay-a
    address: 127.0.0.1:4433

forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: echo
    egress: relay-b
    route:
      - relay-a
      - relay-b
```

Rules:

- `servers` are direct upstreams the client can dial.
- `route[0]` must be one of the configured `servers`.
- `egress`, if set, must match the final route hop.
- Without an explicit `route`, multi-upstream clients must set `egress` to a configured server ID.

### Server / Relay Mode

Server configs use `peers` for relay neighbors.

```yaml
node_id: relay-a
mode: server

identity:
  cert: ./dev/certs/relay-a.crt
  key: ./dev/certs/relay-a.key
  ca: ./dev/certs/ca.crt

listen: 127.0.0.1:4433

peers:
  - id: relay-b
    address: 127.0.0.1:4434
    dial: true
```

Peer rules:

- `peers[].id` is the expected authenticated node ID.
- `peers[].address` is required when `dial: true`.
- `dial: true` means this relay initiates/maintains the peer connection.
- For now, configure `dial: true` on only one side of a peer relationship.
- The non-dialing side may list the peer without `dial` or with `dial: false`:

```yaml
peers:
  - id: relay-a
```

### Services

Services are defined on egress servers/relays:

```yaml
services:
  - name: echo
    protocol: tcp
    target: 127.0.0.1:9000
    peers:
      - relay-a
```

`services[].peers` authorizes authenticated peer node IDs. If omitted or empty, any authenticated peer may use the service.

For multi-hop, the egress currently sees the previous relay as the authenticated peer. Example:

```text
client-1 -> relay-a -> relay-b -> service
```

The egress service on `relay-b` should authorize `relay-a`, not `client-1`, under the current hop-by-hop model.

## Route Semantics

`ConnectRequest.Route` carries the remaining explicit path beginning with the node currently receiving the request.

Example initial client request:

```text
route = [relay-a, relay-b, relay-c]
egress = relay-c
```

At `relay-a`:

```text
route[0] == relay-a
next hop = relay-b
forwarded route = [relay-b, relay-c]
```

At `relay-b`:

```text
route[0] == relay-b
next hop = relay-c
forwarded route = [relay-c]
```

At `relay-c`:

```text
route[0] == relay-c
len(route) == 1, so act as egress
```

If both `route` and `egress` are set, `egress` must equal the final route hop.

## Protocol State

The binary protocol frame envelope is:

```text
version uint8
type    uint8
length  uint16 big endian
payload []byte
```

Current message types:

```go
TypeConnectRequest  = 1
TypeConnectResponse = 2
```

### ConnectRequest

Current payload fields:

```text
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

### ConnectResponse

Current payload fields:

```text
status      uint8   // 0 = OK, 1 = error
code        uint8
message_len uint16 big endian
message     []byte
```

Current response codes:

```text
OK
SERVICE_NOT_FOUND
ACCESS_DENIED
TARGET_DIAL_FAILED
UNSUPPORTED_PROTOCOL
UNREACHABLE_EGRESS
ROUTE_INVALID
NEXT_HOP_UNREACHABLE
INTERNAL_ERROR
```

No backward compatibility has been preserved or attempted. This is intentional while the project is not deployed.

## Peer Session Behavior

The server runtime now has an internal `serverRuntime` and `peerSessions` manager.

Peer behavior today:

- Server starts listener.
- Server creates peer session manager from `cfg.Peers`.
- Server dials `dial: true` peers at startup.
- Outbound peer QUIC connections are passed back to the server runtime so they also get an `AcceptStream` loop.
- Inbound listener connections use the same connection handling path.
- If the authenticated node ID is listed in `peers`, the inbound connection is registered as a reusable peer session.
- Forwarding uses `peerSessions.OpenStream(nextHop)`.
- Each proxied TCP connection still maps to one QUIC stream.

Duplicate peer sessions:

- Mutual dialing is unsupported.
- First live session wins.
- Duplicate session is logged and closed with reason `duplicate peer session unsupported`.

## Runtime Backoff / Shutdown Behavior

Client upstream reconnect:

- active streams close when upstream QUIC connection dies
- listeners remain open
- reconnect is on demand for future local TCP connections
- failed dials back off:

```text
500ms, 1s, 2s, 4s, 8s, 16s, capped at 16s
```

Server accept loop:

- transient QUIC accept failures do not immediately shut down the server
- accept failure backoff starts at `100ms`
- doubles on consecutive failures
- caps at `30s`
- resets after successful accept

Shutdown:

- active connection/stream goroutines are waited on with a default timeout of `5s`

## Demos / Tests

Primary commands:

```sh
make test
make demo-e2e
make demo-multihop
make demo-threehop
```

Demos:

- `make demo-e2e`
  - one-hop
  - `client-1 -> server-1 -> echo`
- `make demo-multihop`
  - two relay hops
  - `client-1 -> relay-a -> relay-b -> echo`
- `make demo-threehop`
  - three relay hops
  - `client-1 -> relay-a -> relay-b -> relay-c -> echo`

Dev cert generation now creates certs for:

```text
client-1
server-1
relay-a
relay-b
relay-c
```

Example config files:

```text
examples/config/client.yaml
examples/config/server.yaml
examples/config/client-multihop.yaml
examples/config/client-threehop.yaml
examples/config/relay-a.yaml
examples/config/relay-b.yaml
examples/config/relay-b-threehop.yaml
examples/config/relay-c.yaml
```

## Current Package Layout

```text
cmd/qoru/              CLI entrypoint
internal/cli/          Cobra commands and command wiring
internal/client/       client runtime, upstream sessions, local TCP proxying
internal/config/       config structs, loading, validation
internal/identity/     TLS/mTLS identity loading and peer identity extraction
internal/protocol/     custom binary frame protocol
internal/server/       server runtime, peer sessions, relay forwarding, TCP services
dev/                   local development helpers and smoke tests
examples/config/       example configs
docs/                  design docs
```

Notable server files:

```text
internal/server/server.go      server runtime orchestration
internal/server/connection.go  connection/stream handling and relay forwarding
internal/server/peers.go       peer session manager
internal/server/proxy.go       stream/TCP proxy helpers
internal/server/policy.go      service authorization
```

## Recommended Next Slices

### 1. Add request IDs / better structured logs

Now that multi-hop works, logs need better correlation.

Suggested fields:

```text
request_id
conn_id
stream_id
peer_id
current_node
service
egress
route
next_hop
response_code
```

This can be local-only at first and does not need a protocol change.

### 2. Improve peer reconnect behavior

Currently `dial: true` peer failure during startup fails server startup. Consider making relays more resilient:

- start server even if peer unavailable
- reconnect peers in background or on demand
- route requests fail with `NEXT_HOP_UNREACHABLE` until peer is connected
- use exponential backoff similar to client upstream reconnect

### 3. Harden inbound peer / relay policy

Current behavior:

- unknown mTLS-authenticated nodes may connect
- if not listed in `peers`, they are not registered as relay peer sessions
- service access is still controlled by `services[].peers`

Questions to settle:

- Should relay forwarding requests require previous hop to be listed in `peers`?
- Should non-peer clients be allowed to connect to a relay for service access?
- Do we need separate concepts for `clients` vs `peers`?

### 4. Improve duplicate peer-session diagnostics / validation

Mutual dialing is unsupported, but this can only be partially validated locally. Possible improvements:

- clearer logs when duplicate is rejected
- status/diagnostic command later
- docs/examples emphasizing one dialer per peer relationship

### 5. Service routing semantics

The desired end-state is service-first routing with optional constraints:

```yaml
service: postgres-prod
# optional
egress: relay-b
# optional
route: [relay-a, relay-b]
```

Future behavior:

- no `egress` / no `route`: qoru chooses eligible egress/path
- `egress`: qoru chooses path to that egress
- `route`: client pins exact path

No automatic routing exists yet.

### 6. End-to-end encrypted payload design

Before implementing E2E encryption, write a focused design doc.

Need decisions on:

- replacing raw TCP data phase with framed encrypted data
- HPKE vs direct X25519/HKDF/AEAD
- per-connection key establishment
- visible vs encrypted metadata
- how errors/close semantics work through relays

This is a major architectural change and should be designed before coding.

## High-Level Recommendation

The best immediate next slice is:

```text
request IDs and structured multi-hop logging
```

Reason:

- low risk
- no protocol change needed
- improves debugging for all following work
- especially useful before changing peer reconnect and routing behavior
