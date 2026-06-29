# qoru Handoff Notes

## Current State

`qoru` is an experimental QUIC-based TCP relay/proxy. The current implementation supports authenticated one-hop TCP forwarding over QUIC/mTLS:

```text
local TCP client -> qoru client -> QUIC/mTLS -> qoru server -> TCP target
```

Each proxied local TCP connection maps to one QUIC stream. Stream setup uses the custom framed control protocol:

```text
ConnectRequest
ConnectResponse
raw TCP bytes...
```

After a successful `ConnectResponse`, payload bytes are proxied directly and are not framed by qoru.

Current major capabilities:

- Cobra CLI: `client`, `server`, `print-config`.
- YAML config.
- QUIC transport via `quic-go`.
- mTLS with configured private CA.
- SPIFFE-style URI SAN node identities.
- Multiple client-side local TCP forwards.
- Multiple direct upstream servers from one client process.
- One reconnecting upstream QUIC session per configured upstream server.
- Forward-level upstream selection via `egress`.
- Multiple named server-side TCP services.
- Per-service peer authorization.
- One QUIC stream per proxied TCP connection.
- Server-side TCP target dialing and byte proxying.
- Local echo-server demo and automated e2e smoke test.

## Current Client Config Shape

The client uses only `servers`; the older singular `server` shape has been removed.

Single upstream example:

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

Multiple upstream example:

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

Validation rules:

- client mode requires at least one `servers` entry
- each `servers[]` entry requires `id` and `address`
- server IDs must be unique
- each forward requires `protocol: tcp`, `listen`, and `service`
- with one upstream server, forward `egress` may be omitted
- with multiple upstream servers, forward `egress` is required
- if set, forward `egress` must match a configured server ID

## Reconnect Semantics

The qoru client keeps local TCP listeners open when an upstream QUIC connection fails.

Current behavior:

```text
upstream QUIC connection dies
active TCP streams on that QUIC connection close
local listeners stay open
future local TCP connections reconnect on demand
```

Important limitation:

```text
active TCP connections are not resumed across reconnect
```

Resuming active TCP streams across a new QUIC connection would require a significantly larger application-level session/resumption protocol with stream IDs, framed data, buffering, ordering, and server-side suspended-session state.

Reconnect is on demand. There is no background reconnect loop. Local TCP handlers do not sleep during reconnect backoff.

Reconnect backoff after failed upstream dials:

```text
500ms, 1s, 2s, 4s, 8s, 16s, capped at 16s
```

During backoff, new local TCP connections fail fast and are closed without qoru writing diagnostic bytes into the TCP stream.

## Logging / Observability

Runtime logs go to stderr. `print-config` writes YAML to stdout.

Client-side reconnect/service setup logging:

- upstream reconnect attempts after previous failures: `Info`
- failed upstream reconnect dials: `Warn`
  - includes `server_id`, `addr`, `backoff`, `next_attempt`, `error`
- successful reconnects after previous failures: `Info`
- service/policy rejections: `Warn`
- local connection failures caused by reconnect backoff: `Warn`
  - includes `server_id`, `addr`, `next_attempt`
- other stream setup or transport failures: `Error`

Server-side lifecycle logging currently includes:

- server listening
- peer connected
- peer disconnected
- connect requested
- target connected
- TCP proxy closed
- failure/rejection cases

## Code Organization

Current package layout of note:

```text
cmd/qoru/              CLI entrypoint
internal/cli/          Cobra commands and command wiring
internal/client/       client runtime and local TCP proxying
internal/config/       config structs, path resolution, YAML load/marshal, validation
internal/identity/     TLS and mTLS identity loading
internal/protocol/     custom binary frame protocol
internal/server/       QUIC server runtime and TCP proxying
```

`internal/client` is split as:

```text
client.go    runtime orchestration, local TCP listeners, local connection handling
session.go   upstream session selection, reconnect, backoff
stream.go    QUIC dial and ConnectRequest/ConnectResponse stream setup
proxy.go     byte proxying between local TCP and QUIC streams
```

Removed older test/convenience helpers:

- `Connect`
- `ConnectTCP`

The lower-level connection primitive is now `ConnectToServer`.

## Current Non-Goals / Not Implemented

Not implemented yet:

- multi-hop forwarding
- end-to-end application payload encryption
- framed data phase after connect setup
- active TCP stream resumption across upstream reconnect
- UDP forwarding
- topology discovery/status commands
- service discovery
- load balancing
- failover across multiple upstreams
- configurable log level/log format
- configurable timeouts/backoff

## Good Next-Step Options

### Option A: Service Selection Semantics

Now that clients can configure multiple direct upstream servers, the next major design topic is how forwards should choose upstreams.

Current behavior is explicit and simple:

```text
forward.egress -> configured upstream server ID
```

Possible future directions:

1. Keep `egress` explicit and required for multi-upstream clients.
2. Add failover lists:

   ```yaml
   egresses: [server-1, server-2]
   ```

3. Add load-balanced service routing.
4. Allow service-name-based selection if multiple upstreams expose the same service.
5. Add health checks or passive failure tracking.

Questions to settle:

- Should `service` names be global or local to each upstream/egress?
- Should a forward be allowed to omit `egress` when multiple upstreams exist?
- Should qoru fail over automatically if the selected upstream is down?
- If failover is added, should it happen per new TCP connection only?
- Should failover be attempted when the server rejects a service, or only when the upstream is unreachable?

Recommended conservative next slice:

- keep current explicit `egress` behavior
- discuss and document desired future semantics before adding failover/load balancing

### Option B: Protocol Response Codes

`ConnectResponse` now carries a machine-readable response code in addition to OK/message:

```go
OK bool
Code ConnectCode
Message string
```

Current codes:

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

Benefits:

- better client logging
- easier tests
- future failover decisions can distinguish policy denial from temporary target dial failure
- less reliance on message strings

This is the first step toward explicit multi-hop routing because future relay decisions can distinguish routing failures from policy and target failures.

### Option C: Configurable Runtime Policy

Currently hardcoded:

- client QUIC dial timeout: `10s`
- server TCP target dial timeout: `10s`
- shutdown wait timeout: `5s`
- reconnect backoff schedule/cap
- log level/format

Potential config shape:

```yaml
runtime:
  log_level: info
  log_format: text
  shutdown_timeout: 5s

timeouts:
  quic_dial: 10s
  target_dial: 10s

reconnect:
  backoff: [500ms, 1s, 2s, 4s, 8s, 16s]
```

Benefits:

- more operationally useful
- easier to tune for real deployments

Costs:

- config expansion before behavior is fully settled
- validation complexity

Recommendation:

- defer until service routing and protocol error semantics are clearer

### Option D: Server-Side Lifecycle/Session Hardening

Server now tracks connection and stream goroutines and logs peer disconnect/proxy close, but there is still room to improve.

Possible improvements:

- clearer connection close reasons
- more structured stream IDs/request IDs in logs
- better shutdown/drain behavior
- active stream counters
- passive health/status command later

Recommended next slice if choosing this path:

- add per-stream request IDs in server/client logs without changing protocol
- use incrementing local counters per process/connection

### Option E: Multi-Hop / E2E Encryption Design

This is the long-term direction but should probably not be implemented immediately.

Before coding multi-hop, design needs to settle:

- path representation
- hop-by-hop vs end-to-end control metadata
- encrypted payload frame format
- key agreement/session establishment
- relay authorization model
- whether raw TCP after setup must be replaced with framed data

Recommendation:

- write a dedicated design doc before implementation

## Recommended Immediate Next Discussion

The strongest next discussion is **service selection semantics**.

The project now has the foundation for direct multi-upstream forwarding. Before adding failover/load balancing, decide what `service` and `egress` should mean long-term:

```text
explicit egress only?
service-based routing?
failover list?
load balancing?
```

A conservative implementation path would be:

1. Keep explicit `egress` as the only supported multi-upstream selection mode.
2. Use the existing protocol response codes so future routing decisions are not string-based.
3. Add route config/validation for a first explicit-route multi-hop version.
4. Then add optional failover lists for new local TCP connections only.
