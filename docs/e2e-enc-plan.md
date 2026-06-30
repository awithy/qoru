# E2E Encryption and Service-First Routing Plan

This plan breaks the next roadmap work into small slices. The goal is to add static service-first routing with multiple possible egress candidates, then add service-bound end-to-end payload encryption.

The target model is:

```text
routing chooses where
service identity proves what
E2E crypto protects payload
```

## Principles

- Implement static service routing before dynamic topology or advertisement.
- Select a concrete `service + egress + route` per local TCP connection before opening the QUIC stream.
- Bind E2E encryption to service identity, not node identity.
- Do not share service private keys across egress nodes.
- Allow multiple egress nodes to hold distinct service cert/key pairs asserting the same service URI SAN.
- Keep dynamic topology, service advertisement, health-aware routing, and UDP out of this phase.

## Target Identity Model

Node identity remains hop-by-hop mTLS:

```text
spiffe://qoru/node/relay-b
```

Service identity is used for E2E payload encryption:

```text
spiffe://qoru/service/echo
```

Multiple egress nodes may serve the same service using different private keys:

```text
relay-b service cert: URI:spiffe://qoru/service/echo
relay-c service cert: URI:spiffe://qoru/service/echo
```

The client verifies that the selected egress proves possession of a trusted service certificate for the requested service.

## Progress Summary

Completed:

- static `routes` config shape and validation
- client route resolution from forwards to static candidates
- ordered setup-time candidate fallback
- service identity config, cert parsing/verification helpers, and dev service cert generation
- protocol frame scaffolding for E2E hello/data/close frames

Implemented now:

- runtime required-E2E negotiation through forward `e2e: off|auto|always` / `ConnectRequest.E2ERequired`
- authenticated E2E handshake
- encrypted payload proxying with E2E data frames
- E2E policy enforcement for configured services

Next slice: broader E2E hardening, multi-hop smoke coverage, and operational polish.

## Slice 1: Static Route-Candidate Config

Status: implemented.

Added config structs and validation for static service route candidates.

Example future shape:

```yaml
routes:
  - service: echo
    protocol: tcp
    selection: ordered
    candidates:
      - egress: relay-b
        route: [relay-a, relay-b]
      - egress: relay-c
        route: [relay-a, relay-c]
```

Validation:

- route service is required
- protocol is currently `tcp`
- at least one candidate is required
- each candidate has `egress`
- each candidate route is non-empty
- candidate route final hop matches candidate egress
- candidate route first hop matches a configured client `servers[]` entry
- selection is valid; start with `ordered`, possibly add `round_robin` and `random` later

## Slice 2: Client Route Resolution

Status: implemented.

The client resolves each forward to one or more selected route candidates.

A forward should eventually be able to identify only the service:

```yaml
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: echo
```

Resolution precedence:

1. explicit forward `route`
2. explicit forward `egress`
3. static route candidate lookup by `protocol + service`
4. current fallback rules

Each accepted local TCP connection should resolve to something like:

```go
type SelectedRoute struct {
    Service string
    Egress  string
    Route   []string
}
```

The existing `ConnectRequest` fields are used once a concrete candidate is selected.

## Slice 3: Candidate Selection and Setup-Time Fallback

Status: implemented for `ordered` setup-time fallback. Non-ordered policies are not implemented yet.

Start with simple `ordered` selection.

Behavior:

- pick a candidate before opening the stream
- if setup fails before payload proxying starts, optionally try the next candidate
- do not fail over after payload proxying begins

Likely retryable setup failures:

- upstream reconnect/backoff
- next-hop unreachable
- unreachable egress

Likely non-retryable setup failures:

- access denied
- service not found
- unsupported protocol
- malformed or invalid route caused by config error

Implemented retryability policy:

- retry: transport/open-stream errors, reconnect/backoff, `UNREACHABLE_EGRESS`, `NEXT_HOP_UNREACHABLE`, `TARGET_DIAL_FAILED`
- do not retry: `ACCESS_DENIED`, `SERVICE_NOT_FOUND`, `UNSUPPORTED_PROTOCOL`, `ROUTE_INVALID`, `INTERNAL_ERROR`

This can be tuned later if needed.

## Slice 4: Service Identity Certificate Plumbing

Status: implemented for config loading/validation, identity helpers, development service certificate generation, and required-E2E runtime use.

Add service identity configuration and certificate helpers, without encrypting payload yet.

Possible server service config:

```yaml
services:
  - name: echo
    protocol: tcp
    target: 127.0.0.1:9000
    e2e:
      cert: ./dev/certs/relay-b-echo.crt
      key: ./dev/certs/relay-b-echo.key
      ca: ./dev/certs/service-ca.crt
```

Possible client trust config:

```yaml
service_identity:
  ca: ./dev/certs/service-ca.crt
```

Add helpers to:

- parse `spiffe://qoru/service/<name>` URI SANs
- load service cert/key
- verify service cert chain
- verify that the service cert contains the requested service URI SAN

Update dev cert generation to create multiple distinct service certs with the same service identity, for example:

```text
relay-b-echo.crt -> spiffe://qoru/service/echo
relay-c-echo.crt -> spiffe://qoru/service/echo
```

Each cert should have its own private key.

## Slice 5: Protocol Frame Scaffolding for E2E

Status: implemented in the protocol package and used by required-E2E runtime streams.

Make the stream capable of carrying framed post-connect E2E messages.

Potential new frame types:

```text
TypeE2EClientHello
TypeE2EServerHello
TypeE2EClientFinished
TypeE2EData
TypeE2EClose
```

Encrypted mode should be explicit. This may require adding a request capability/flag such as:

```text
ConnectRequest.E2ERequired = true
```

The current binary `ConnectRequest` format is fixed, so this may require a protocol bump or a new connect-request type. Since qoru is experimental, prefer clean protocol evolution over compatibility workarounds.

## Slice 6: Authenticated E2E Handshake

Status: implemented for required-E2E TCP streams. qoru does not support a runtime mode that performs the E2E handshake and then carries plaintext payload.

Implement the E2E identity handshake before encrypted payload proxying.

Goals:

- client sends client node cert or equivalent identity proof
- client sends ephemeral public key
- egress sends service cert chain
- egress sends ephemeral public key
- egress signs the full handshake transcript in `E2EServerHello`
- client signs the full handshake transcript in `E2EClientFinished`
- client verifies service identity matches `spiffe://qoru/service/<service>`
- egress verifies original ingress client identity
- both sides derive per-connection traffic keys
- logs show handshake success/failure

Transcript binding should include at least:

- `request_id`
- `service`
- selected `egress`
- selected `route`
- client ephemeral key
- egress ephemeral key

This protects against replay or confused-context handshakes.

Implemented core pieces:

- X25519 ephemeral key generation and shared-secret derivation
- canonical transcript hashing
- client node certificate verification against the node CA
- service certificate verification against `spiffe://qoru/service/<service>`
- RSA/ECDSA/Ed25519 certificate signature verification helpers
- HKDF-derived directional traffic keys for later encrypted records

## Slice 7: Encrypted Data Frames

Status: implemented for required-E2E TCP streams.

Replace raw post-connect TCP bytes with encrypted payload frames.

Current stream shape:

```text
[ConnectRequest frame]
[ConnectResponse frame]
[raw TCP bytes...]
```

Future encrypted stream shape may be:

```text
[ConnectRequest frame]
[ConnectResponse or E2E-ready frame]
[E2E ClientHello frame]
[E2E ServerHello frame]
[encrypted data frame]
[encrypted data frame]
...
```

Implementation direction:

```go
type EncryptedReader struct { ... }
type EncryptedWriter struct { ... }
```

The implemented record layer uses AES-GCM with handshake-derived directional 32-byte keys, 8-byte big-endian sequence nonce suffixes, strict in-order sequence checks, transcript-bound AEAD associated data, and `E2EClose` for encrypted-mode EOF signaling. Then the existing proxying model can remain conceptually similar, but local TCP is proxied to encrypted stream wrappers instead of directly to the raw QUIC stream.

Open design items:

- exact runtime frame ordering and negotiation
- whether handshake readiness is carried in existing response frames or new frame types
- runtime half-close integration over encrypted frames
- runtime error mapping and logging

## Slice 8: Require and Enforce E2E Policy

Status: implemented for services configured with `services[].e2e` and forwards configured with `e2e: auto` or `e2e: always`.

Once encrypted payload frames work, add policy controls.

Forward-side config:

```yaml
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: echo
    e2e: auto
```

Modes:

- `off` or omitted: do not use E2E frames
- `auto`: use E2E only when the selected route has an intermediary relay (`len(route) > 1`)
- `always`: use E2E for direct and relayed routes

Service-side behavior:

- if a request has `E2ERequired`, the selected service must have `services[].e2e` configured
- if a relayed/routed request does not have `E2ERequired` and the selected service has `services[].e2e`, reject plaintext
- direct one-hop plaintext remains allowed for E2E-capable services so `e2e: auto` can skip redundant frame encryption
- final E2E service authorization uses original client identity from the E2E handshake
- target dialing for E2E streams happens only after successful E2E authentication and authorization

This separates policy cleanly:

```text
Relay authorization:       can this previous hop forward through this node?
Service authorization:     can this original client use this service?
```

## Recommended Implementation Order

1. Static route-candidate config.
2. Client route resolution.
3. Ordered candidate fallback.
4. Service identity cert plumbing.
5. Protocol E2E frame scaffolding.
6. Authenticated E2E handshake.
7. Encrypted data frames.
8. Require/enforce E2E policy.

## Deferred Work

These are intentionally out of scope for this phase:

- dynamic service advertisement
- topology discovery/status/control plane
- health-aware route selection
- load/cost-aware routing
- active TCP session migration across reconnect
- UDP support
- broad observability/configurability hardening
