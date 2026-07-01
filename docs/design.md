# qoru Design

## Overview

`qoru` is an experimental QUIC-based network relay/proxy. The current implementation supports authenticated TCP forwarding over QUIC/mTLS, including direct one-hop forwarding and explicit-route multi-hop forwarding:

```text
TCP client -> qoru client -> QUIC/mTLS -> qoru server -> TCP target
TCP client -> qoru client -> relay-a -> relay-b -> TCP target
```

The long-term direction is a chainable relay overlay with service-first routing and end-to-end payload encryption. The current code uses static config, one or more configured direct upstream servers, TCP forwarding, QUIC transport, mTLS authentication, explicit relay routing, static service route candidates, and optional required E2E payload encryption for configured services.

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
- Static service route candidates with ordered setup-time candidate fallback.
- On-demand upstream reconnect for new local TCP connections after connection loss.
- One QUIC stream per proxied local TCP connection.
- Multiple local TCP forwards.
- Server support for multiple streams per QUIC connection.
- Server-side named TCP services with per-service peer authorization.
- Explicit relay ingress authorization with `allowed_relay_clients`.
- Routed egress peer authorization through top-level `peers`.
- Optional one-hop egress selection; selected egress must currently be the connected server unless an explicit route is provided.
- Explicit-route multi-hop TCP forwarding through configured next-hop servers.
- Server-side TCP target dialing with timeout and basic service target address validation.
- `ConnectResponse` success/failure handshake before raw TCP proxying begins.
- Half-close-aware bidirectional byte proxying between local TCP, QUIC streams, and server-side TCP targets.
- Service identity config, service certificate verification helpers, startup service-certificate validation/cache, and development service certificate generation.
- Protocol frames for E2E hello, encrypted data, and close messages.
- Runtime required E2E mode for TCP forwards and services using service identity certificates.
- Authenticated E2E handshake with client node proof, egress service proof, encrypted payload records, and classified E2E setup errors.

Not implemented yet:

- resuming active proxied TCP connections across upstream QUIC reconnects
- dynamic topology/service advertisement
- optional/non-required E2E negotiation
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

1. resolve the forward to one or more route candidates, using explicit forward `route`/`egress` first and then top-level static `routes` by `protocol + service`
2. generate a UUIDv7 request ID
3. try route candidates in order for `selection: ordered`
4. open a new QUIC stream on the candidate's first-hop upstream connection
5. send `ConnectRequest{RequestID: "...", Protocol: "tcp", Service: "...", Egress: "...", Route: [...]}`
6. read `ConnectResponse`
7. if setup fails with a retryable setup error before proxying begins, try the next candidate
8. if E2E is required, run the service-identity handshake and switch to encrypted E2E frames
9. proxy bytes between the local TCP connection and the selected stream mode

### `qoru server`

Loads and validates server config, loads its TLS identity, validates configured E2E service certificates, starts a QUIC listener, accepts QUIC connections, accepts multiple streams per connection, reads `ConnectRequest`, resolves and authorizes the requested service, and proxies bytes. Plaintext service streams dial the target before `ConnectResponse OK`; E2E streams send `ConnectResponse OK`, authenticate the original client, dial the target before `E2EServerHello`, and then exchange encrypted E2E frames.

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

Conceptually:

- `service` is what the client wants.
- `egress` optionally constrains where traffic must exit.
- `route` optionally constrains the exact hop sequence.
- if neither `egress` nor `route` is set, qoru may choose from configured or discovered candidates.
- if `route` is set, the last hop is the egress node.
- if both `route` and `egress` are set, they must agree.
- authorization policy can still reject any automatic, selected, or explicit routing decision.

This model supports both automatic routing and client-specified routing without making either one mandatory. The current implementation is intentionally narrower: service names are resolved on the selected direct upstream server, and with multiple configured upstream servers the client must set `egress` explicitly.

### Static service route candidates

qoru includes a minimal static service-first routing layer, not full dynamic topology discovery. The client can map a requested service to one or more candidate egress paths and select one candidate per local TCP connection before opening the QUIC stream.

Example shape:

```yaml
routes:
  - service: echo
    candidates:
      - egress: relay-b
        route: [relay-a, relay-b]
      - egress: relay-c
        route: [relay-a, relay-c]
```

Then a forward can identify only the service:

```yaml
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: echo
```

For each accepted local connection, the client selects one candidate and sends the existing logical request fields:

```text
service = echo
egress  = relay-b
route   = [relay-a, relay-b]
```

Current candidate selection supports ordered setup-time failover. Future policies may add round-robin or random selection. There is no per-byte overhead and no relay behavior change required as long as the client resolves the concrete `egress` and `route` before sending `ConnectRequest`.

Once a connection has started proxying, qoru should not attempt mid-connection failover. If setup fails before proxying begins, the client may try another candidate depending on selection policy and failure class.

This static candidate model deliberately avoids dynamic service advertisement, health-aware route discovery, topology maps, and load/cost-aware routing for now. Those capabilities can be added later as a control plane. Even when dynamic advertisement exists, advertised service reachability should be treated as a hint; the egress still needs to prove service identity during the end-to-end encryption handshake.

### Explicit constraints

A client may still constrain the egress node while allowing qoru to choose or look up a path to that egress:

```yaml
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: postgres-prod
    egress: relay-b
```

In this mode, `egress` means "the service must exit at this node." It is a routing constraint, not the service identity itself.

For multi-hop topologies, a client may specify an explicit route:

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

The client requests a named service. Service names are currently resolved on the selected direct upstream server for one-hop requests, or on the final route hop for explicit-route requests. `egress` is optional when exactly one upstream server is configured and no explicit route is used; empty means that server may satisfy the request. When multiple upstream servers are configured and no explicit route is used, each forward must set `egress` to one configured server ID. For explicit routes, the first route hop selects the direct upstream server, and `egress`, if set, must match the final route hop. A forward may set `e2e: off|auto|always`; `auto` requires E2E only when the selected route has an intermediary relay (`len(route) > 1`), while `always` requires E2E even for direct one-hop traffic.

Static service route candidate configuration is implemented in config loading, validation, and basic client runtime resolution. For `selection: ordered`, the client tries matching route candidates in order. If stream setup fails before payload proxying begins with a retryable setup error, the client tries the next candidate. Candidate fallback does not happen after payload proxying begins. Non-ordered selection policies are planned later.

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

When a relay receives a routed request, the route is interpreted as the remaining path beginning with the current node. The relay validates `route[0] == node_id`.

Relay authorization is explicit:

- if more hops remain, the authenticated previous hop must be listed in `allowed_relay_clients`; if the list is omitted or empty, any authenticated node may use this server as an intermediate relay
- when a routed request reaches its egress node (`len(route) == 1`), the authenticated previous-hop relay must be listed in top-level `peers`
- final local service access is checked separately with `services[].peers`

If more hops remain, the relay opens a stream to `route[1]`, forwards the request with the current hop removed, and proxies stream bytes. For plaintext requests, intermediary relays can observe raw proxied bytes. For `E2ERequired` requests, relays only observe opaque E2E handshake/data/close frames after setup.

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

One-hop service egress:

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

Intermediate relay:

```yaml
node_id: relay-a
mode: server

identity:
  cert: ./dev/certs/relay-a.crt
  key: ./dev/certs/relay-a.key
  ca: ./dev/certs/ca.crt

listen: 127.0.0.1:4433

allowed_relay_clients:
  - client-1

peers:
  - id: relay-b
    address: 127.0.0.1:4434
    dial: true
```

Routed egress relay:

```yaml
node_id: relay-b
mode: server

identity:
  cert: ./dev/certs/relay-b.crt
  key: ./dev/certs/relay-b.key
  ca: ./dev/certs/ca.crt

listen: 127.0.0.1:4434

peers:
  - id: relay-a

services:
  - name: echo
    protocol: tcp
    target: 127.0.0.1:9000
    peers:
      - relay-a
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
- `forwards[].e2e` may be omitted or set to `off`, `auto`, or `always`; `auto`/`always` require `service_identity.ca`
- `route` is optional; when set, it must be non-empty and contain no empty hops
- route length is capped at `3` hops for the first multi-hop implementation
- the first route hop must match a configured direct upstream server
- if both `route` and `egress` are set, `egress` must match the final route hop
- optional top-level `routes` entries define static service route candidates for client runtime selection; each entry requires `service`, `protocol: tcp`, optional `selection: ordered`, and at least one candidate
- each static route candidate requires `egress` and non-empty `route`; the first hop must match a configured direct upstream server and the final hop must match `egress`

Server required fields:

- `mode: server`
- `listen`

Server service and relay fields:

- `routes` is client-mode only and is rejected in server mode.
- `service_identity.ca`: optional service-identity CA bundle path. Required when any service configures `e2e`.
- `services`: named protocol-aware services this server can provide. Each service has `name`, `protocol`, `target`, optional `peers`, and optional `e2e`. If `peers` is omitted or empty, any authenticated peer may use that service.
- `peers`: optional configured relay peers. Each peer has `id`, optional `address`, and optional `dial`. Conceptually, `peers` represents allowed overlay neighbors. `dial: true` means this node should initiate/maintain an outbound connection to that peer; `address` is required when `dial: true`. `dial: false` or omitted means the peer relationship may be inbound-only. Routed egress requests require the previous-hop relay to be listed here.
- `allowed_relay_clients`: optional list of authenticated node IDs allowed to use this node as an intermediate relay. A routed request with more than one remaining hop is rejected unless the previous hop appears in this list. If omitted or empty, any authenticated node may use this server as an intermediate relay.
- `services[].e2e`: optional service identity certificate/key configuration for required E2E mode. When set, both `cert` and `key` are required, `service_identity.ca` is required, and the service protocol must currently be `tcp`. Plaintext routed requests to a service configured with `e2e` are rejected; direct one-hop plaintext is allowed so forward `e2e: auto` can avoid redundant frame encryption.

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

## End-to-End Service Identity and Payload Encryption

Service identity certificate plumbing is implemented for config loading, validation, development certificate generation, startup service-certificate validation/cache, and required-E2E runtime handshakes.

The end-to-end encryption model separates node identity from service identity:

```text
QUIC/mTLS identity:     spiffe://qoru/node/relay-b
E2E payload identity:   spiffe://qoru/service/postgres-prod
```

QUIC/mTLS remains hop-by-hop and authenticates adjacent nodes. End-to-end payload encryption should be bound to the requested service, not to a specific egress node. This allows multiple egress nodes to serve the same logical service without sharing a private key.

Each eligible egress node may have its own service certificate and private key. Multiple certificates may assert the same service URI SAN, for example:

```text
relay-b service cert: URI:spiffe://qoru/service/echo
relay-c service cert: URI:spiffe://qoru/service/echo
```

The client verifies that the selected egress proves possession of a private key for a certificate trusted for `spiffe://qoru/service/<service>`. The service certificate proves "what service is being terminated," while the route and mTLS layer determine "where the stream is forwarded."

The design should skip a shared service private-key model. Sharing one service private key across multiple egress nodes is simpler, but it increases blast radius, complicates rotation/revocation, and weakens isolation between egress nodes.

### E2E handshake target

For each E2E-required proxied connection, after route selection but before application payload proxying, ingress and egress perform an authenticated ephemeral key exchange through the selected route:

```text
client -> egress, via relays: ConnectRequest(service, egress, route, ...)
client <-> egress: E2E handshake messages
egress proves service identity with service cert chain and ephemeral key
client proves original ingress identity with client node cert and ephemeral key
client verifies service identity == spiffe://qoru/service/<service>
client and egress derive per-connection traffic keys
encrypted framed payload begins
```

Both sides bind signatures and key derivation to the handshake transcript, including `request_id`, `service`, selected `egress`, selected original `e2e_route`, client ephemeral key, and egress ephemeral key. This prevents relays or confused endpoints from replaying a hello into a different request context.

The egress authenticates the original ingress client at the E2E layer, not only the previous-hop relay observed through mTLS. This preserves separate policy layers:

```text
Relay authorization:       can this previous hop forward through this node?
Service authorization:     can this original client use this service?
```

For plaintext service access, `services[].peers` authorization is based on the authenticated direct peer. For E2E service access, final service authorization evaluates the original client identity from `ClientHello`.

### E2E encrypted data framing

The plaintext stream model switches to raw TCP bytes after `ConnectResponse`. End-to-end payload encryption replaces raw bytes with framed encrypted records after the E2E handshake. Intermediary relays continue to forward opaque stream bytes after setup and do not need access to plaintext TCP payloads.

The encrypted stream shape is:

```text
[ConnectRequest frame]
[ConnectResponse OK]
[E2E ClientHello frame]
[E2E ServerHello frame]
[encrypted data frame]
[encrypted data frame]
...
```

Runtime required-E2E stream shape is `ConnectRequest{E2ERequired}`, `ConnectResponse OK`, `E2EClientHello`, `E2EServerHello`, `E2EClientFinished`, then encrypted `E2EData` records and `E2EClose`. If E2E setup fails after `ConnectResponse OK` but before `E2EServerHello`—for example original-client authorization or target dialing fails—the egress sends `E2EClose` with a protocol `ConnectCode`, allowing the ingress to classify the failure and try another setup-time route candidate when policy allows. In encrypted mode, final service authorization and target dialing happen only after the egress authenticates the original ingress client.

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
TypeE2EClientHello = 3
TypeE2EServerHello = 4
TypeE2EData = 5
TypeE2EClose = 6
TypeE2EClientFinished = 7
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
e2e_required uint8 // 0 = false, 1 = true
e2e_route_count uint8
repeated e2e_route_count times:
  hop_len    uint16 big endian
  hop        []byte
```

`route` carries the remaining explicit path beginning with the node currently receiving the request. A relay forwards with the current hop removed, so `[relay-a, relay-b]` becomes `[relay-b]` when `relay-a` forwards to `relay-b`. For E2E requests, `e2e_route` carries the original selected route for transcript binding and is forwarded unchanged by relays.

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

If status is OK, code must be `OK`. For plaintext requests, both sides hand the stream over to raw TCP proxying. For E2E-required requests, both sides run the E2E handshake and then exchange only encrypted E2E frames. If status is error, code identifies the failure class and message provides human-readable detail.

### E2E frames

The protocol package includes frame encoders/decoders for E2E handshakes, encrypted payloads, and encrypted-mode close/error signaling. Required E2E runtime mode uses these frames after a successful `ConnectResponse`.

`E2EClientHello` payload:

```text
cert_chain_count uint8
repeated cert_chain_count times:
  cert_len       uint16 big endian
  cert_der       []byte
key_len          uint16 big endian
ephemeral_key    []byte
signature_len    uint16 big endian
signature        []byte
```

`E2EServerHello` uses the same shape, but the certificate chain is the egress service certificate chain.

`E2EClientFinished` payload:

```text
signature_len uint16 big endian
signature     []byte
```

This third handshake frame lets the client sign the full transcript after receiving the egress ephemeral key.

`E2EData` payload:

```text
nonce_suffix_len uint8
nonce_suffix     []byte
ciphertext_len   uint16 big endian
ciphertext       []byte
```

`E2EClose` payload:

```text
code         uint8
connect_code uint8 // protocol ConnectCode associated with error closes, OK for normal EOF
message_len  uint16 big endian
message      []byte
```

Current E2E frame limits:

```text
MaxE2ECertChainCount     = 8
MaxE2ECertLength         = 16 KiB
MaxE2EEphemeralKeyLength = 4096
MaxE2ESignatureLength         = 8192
MaxE2EFinishedSignatureLength = 8192
MaxE2ENonceSuffixLength       = 24
```

The handshake core uses X25519 ephemeral keys, certificate signatures, a transcript bound to `request_id`, `service`, `egress`, original `e2e_route`, and both ephemeral keys, plus HKDF-derived directional traffic keys. The encrypted record-layer helper uses AES-GCM, directional keys, 8-byte big-endian sequence nonce suffixes, strict in-order sequence checks, transcript-bound associated data, and `E2EClose` for encrypted-mode EOF/error signaling. Error closes carry a protocol `ConnectCode` so E2E setup/auth/target failures can be classified consistently with plaintext setup failures. Runtime E2E mode is explicit via `ConnectRequest.E2ERequired`, driven by forward `e2e` policy. Broader negotiation and operational hardening remain open design items.

Current TCP stream model:

```text
[ConnectRequest frame]
[ConnectResponse frame]
[raw TCP bytes...]
```

The setup/control phase is framed. Once the server confirms success, the remaining stream bytes are proxied directly between the local TCP connection and target TCP connection.

TCP proxying is half-close-aware. When one copy direction reaches EOF, qoru gracefully closes only the opposite write side and lets the other direction continue so request-then-half-close protocols can still receive responses. Unexpected copy or half-close errors abort both endpoints to unblock the paired copy direction.

Required E2E runtime mode uses framed encrypted data messages after an E2E service-identity handshake. qoru does not support a mode that performs the E2E handshake and then carries plaintext application payload.

## Timeouts and Reconnect Backoff

Timeouts and reconnect policy are currently hardcoded.

- client QUIC dial timeout: `10s`
- server TCP target dial timeout: `10s`
- server QUIC accept failure retry backoff: starts at `100ms`, doubles on consecutive failures, capped at `30s`, and resets after a successful accept
- client upstream reconnect backoff after failed dial attempts: `500ms`, `1s`, `2s`, `4s`, `8s`, `16s`, capped at `16s`
- server relay peer reconnect backoff for `dial: true` peers: `500ms`, `1s`, `2s`, `4s`, `8s`, `16s`, then `16s` forever until reconnect succeeds

Server service target dialing uses `net.Dialer.DialContext` and validates configured service targets with `net.SplitHostPort` before dialing. DNS lookup and dial errors are reported through `ConnectResponse` for plaintext setup and through `E2EClose` with `TARGET_DIAL_FAILED` for required-E2E setup after `ConnectResponse OK`.

The server listener accept loop is resilient to transient QUIC accept failures. If accepting a connection fails while the server context is still active, qoru logs the failure, waits with exponential backoff, and retries instead of immediately shutting down. The backoff resets after a successful accept.

Client reconnect is on demand. qoru does not run a background reconnect loop and does not sleep inside local TCP handlers during backoff. If a local TCP connection arrives while the selected upstream is still in reconnect backoff, stream setup fails fast and the local TCP connection is closed without payload injection.

Relay peer reconnect for `dial: true` peers runs in the background and also reconnects on demand when a forwarded request needs a peer session. During peer reconnect backoff, forwarded requests fail fast with `NEXT_HOP_UNREACHABLE`.

Timeouts and reconnect policy are not yet configurable.

## Logging

Request-scoped logs include `request_id`, a UUIDv7 generated by the ingress client and forwarded unchanged through explicit-route relays. Server-side request logs also include the authenticated `peer_id`, requested `protocol`, `service`, `egress`, and `route`; relay logs include `next_hop` where applicable.

Runtime commands use Go's standard `log/slog` text handler, which emits logfmt-style output:

```text
time=... level=INFO msg="server listening" node_id=server-1 addr=127.0.0.1:4433
```

Runtime logs go to stderr so stdout can remain available for command output.

Client reconnect observability is split between upstream-session events and local TCP setup events:

- upstream reconnect attempts after previous failures are logged at `Info`
- failed upstream reconnect dials are logged at `Warn` with `server_id`, `addr`, `backoff`, and `next_attempt`
- peer reconnect scheduling is logged at `Warn` with `peer_id`, `addr`, `backoff`, and `next_attempt`; repeated backoff sleeps are not logged
- successful reconnects after previous failures are logged at `Info`
- service/policy rejections are logged at `Warn` with machine-readable response codes where applicable
- relay next-hop failures and downstream rejections are logged at `Warn` with response-code fields
- E2E setup failures are logged at `Warn` with `response_code` when available and `e2e_phase` values such as `read_server_hello`, `authorize_client`, or `prepare_service`
- E2E encrypted proxy errors are logged with E2E `close_code`, protocol `response_code`, and close message when present
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
5. for each local TCP connection, resolves ordered route candidates and opens a new QUIC stream to the candidate's first hop
6. sends `ConnectRequest{RequestID: "...", Protocol: "tcp", Service: "...", Egress: "...", Route: [...], E2ERequired: ...}`
7. waits for `ConnectResponse`
8. for E2E-required candidates, runs the service-identity handshake and wraps the stream in encrypted E2E readers/writers
9. proxies bytes between the local TCP connection and the selected stream mode
10. if setup fails before proxying with a retryable error, tries the next candidate
11. exits cleanly when the context is canceled
12. keeps local listeners running if the upstream QUIC connection later fails
13. reconnects the selected upstream on demand when a later local TCP connection needs a new stream

Relevant client package files:

- `client.go` contains runtime orchestration, local TCP listeners, and local connection handling.
- `session.go` contains upstream session selection and reconnect management.
- `stream.go` contains QUIC dialing and the `ConnectRequest`/`ConnectResponse` stream setup handshake.
- `proxy.go` contains client-specific proxy wiring between local TCP connections and QUIC streams.

Current limitation: reconnect is on demand and applies only to future local TCP connections. Active proxied TCP connections are bound to streams on the old QUIC connection; if that connection dies, those TCP connections are closed rather than resumed. Failed reconnect dials use exponential backoff capped at `16s`; successful reconnect resets the backoff state.

## Peer Session Direction Model

The long-term relay model treats node-to-node QUIC connections as logical peer sessions, independent of which side initiated the underlying connection.

Peer config should be understood as an allowed overlay-neighbor relationship, not merely a dial target. In the current implementation, the side that initiates a relay-to-relay connection needs a `peers` entry with `address` and `dial: true`; the listening side should also list the initiating relay without `dial` so routed egress requests from that relay are authorized and the inbound connection can be registered as a reusable peer session. `dial` controls whether a node initiates/maintains the connection or only accepts/registers an inbound session.

A relay/server should eventually:

1. start its QUIC listener
2. initiate outbound QUIC connections to configured peers at startup
3. accept inbound QUIC connections from peers
4. authenticate all peers with mTLS node identity
5. register each connection as a session keyed by remote node ID
6. use the session manager for forwarding to a next hop

In this model, route forwarding asks for an authenticated peer session by node ID rather than dialing the next hop ad hoc for every proxied TCP connection.

For now, do not configure both sides of a peer relationship with `dial: true`. Exactly one side should initiate and the other side should list the peer without `dial` or with `dial: false`. Mutual dialing is intentionally unsupported for now to keep session ownership simple.

Current implementation note: explicit-route relay forwarding uses peer sessions keyed by authenticated node ID. Intermediate relay forwarding requires the authenticated previous hop to be listed in `allowed_relay_clients`; routed egress requires the previous-hop relay to be listed in top-level `peers`. A server attempts to dial configured `dial: true` peers at startup, but startup continues if a peer is unavailable. Dialing peers are maintained by background reconnect loops with capped exponential backoff: `500ms`, `1s`, `2s`, `4s`, `8s`, `16s`, then `16s` forever until reconnect succeeds. Forwarding also reconnects on demand when backoff allows; during backoff, route requests fail fast with `NEXT_HOP_UNREACHABLE`. Accepted inbound connections from configured peers are also registered as reusable peer sessions, so either side can open streams on the same QUIC connection. Inbound connections from nodes not listed in `peers` are still accepted for normal client/service use, but are not registered as relay peer sessions. If a duplicate peer session appears, the first live session wins and the duplicate is logged and closed.

## Server Runtime

The current server runtime lives in `internal/server`.

`server.Run` currently:

1. validates server config
2. loads server mTLS config
3. validates and caches configured E2E service certificates
4. starts a QUIC listener on `cfg.Listen`
5. logs the bound address
6. accepts QUIC connections
7. accepts multiple streams per QUIC connection
8. reads `ConnectRequest` per stream
9. validates that the requested protocol is currently supported (`tcp`)
10. validates optional `egress` against this server's `node_id`
11. handles relay forwarding, plaintext service setup, or required-E2E service setup
12. for plaintext service setup, resolves/authorizes by authenticated peer, dials the target, sends `ConnectResponse`, and proxies raw bytes
13. for E2E service setup, sends `ConnectResponse OK`, authenticates/authorizes the original client, dials the target before `E2EServerHello`, and proxies encrypted E2E records
14. exits cleanly when the context is canceled

For routed requests, the receiving server validates that the first remaining route hop is its own `node_id`. If additional hops remain, the previous hop must be authorized by `allowed_relay_clients`; the relay then opens a stream to the next configured peer, forwards the request with its own hop removed from `route`, relays the downstream `ConnectResponse` back upstream, and then proxies raw bytes between QUIC streams. If no additional hops remain, the server is the routed egress; the previous-hop relay must be listed in top-level `peers` before service resolution and `services[].peers` authorization are evaluated.

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
service-ca.crt
service-ca.key
client-1.crt
client-1.key
server-1.crt
server-1.key
relay-a.crt
relay-a.key
relay-b.crt
relay-b.key
relay-c.crt
relay-c.key
relay-b-echo.crt
relay-b-echo.key
relay-c-echo.crt
relay-c-echo.key
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
make demo-all
```

Or run them individually:

```sh
make demo-e2e
make demo-multihop
make demo-threehop
make demo-e2e-auto-direct
make demo-e2e-encrypted
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
internal/e2e/          E2E handshake transcript, certificate proof, and key-derivation helpers
internal/identity/     TLS and mTLS identity loading
internal/protocol/     custom binary frame protocol
internal/proxyio/      shared half-close-aware bidirectional proxying
internal/requestid/    UUIDv7 request ID generation and validation
internal/server/       QUIC server runtime and TCP proxying
internal/testcert/     generated test certificate helpers for Go tests
dev/echo-server/       local TCP echo target for demos
dev/                   local development helpers
examples/config/       example client/server YAML configs
docs/                  design documentation
```

## Near-Term Next Steps

Current priority is operational polish around service-first routing and end-to-end payload encryption while deferring broader topology/control-plane and UDP work.

Completed in the current service/E2E track:

1. Static service route-candidate config and validation.
2. Client route resolution from forwards to static candidates.
3. Ordered setup-time candidate fallback.
4. Service identity certificates using SPIFFE-style service URI SANs such as `spiffe://qoru/service/echo`.
5. Protocol frames for E2E hello, encrypted data, and close/error signaling.
6. Runtime required-E2E negotiation via forward `e2e` policy / `ConnectRequest.E2ERequired`.
7. Authenticated E2E handshake where the egress proves service identity and the ingress proves original client identity.
8. Encrypted framed payload records between ingress and egress.
9. E2E policy enforcement: services with `services[].e2e` reject routed plaintext requests.
10. E2E setup error classification, ordered candidate fallback for retryable E2E setup failures, and non-retryable access-denied handling.
11. Startup validation/cache for configured service identity certificates.
12. E2E observability with handshake phase, close code, and response-code logging.

Next:

1. Add broader operational documentation and deployment guidance.
2. Add metrics/status surfaces for routes, sessions, reconnects, and E2E streams.
3. Later: dynamic service advertisement/topology, richer health-aware route selection, non-ordered candidate selection policies, configurable timeouts/backoff, and UDP support.
