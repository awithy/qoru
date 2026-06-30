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

## Slice 1: Static Route-Candidate Config

Status: implemented for config loading and validation. No runtime behavior change yet.

Add config structs and validation for static service route candidates. No runtime behavior change yet.

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

Status: implemented for first-candidate static route resolution. Candidate fallback is not implemented yet.

Teach the client to resolve each forward to a selected route candidate.

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

The existing `ConnectRequest` fields can still be used once a concrete candidate is selected.

## Slice 3: Candidate Selection and Setup-Time Fallback

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

This can be tuned after implementation.

## Slice 4: Service Identity Certificate Plumbing

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

Make the stream capable of carrying framed post-connect E2E messages.

Potential new frame types:

```text
TypeE2EClientHello
TypeE2EServerHello
TypeE2EData
TypeE2EClose
```

Encrypted mode should be explicit. This may require adding a request capability/flag such as:

```text
ConnectRequest.E2ERequired = true
```

The current binary `ConnectRequest` format is fixed, so this may require a protocol bump or a new connect-request type. Since qoru is experimental, prefer clean protocol evolution over compatibility workarounds.

## Slice 6: Authenticated E2E Handshake Without Encrypted Payload

Implement the E2E identity handshake first, but temporarily continue proxying plaintext afterward.

Goals:

- client sends client node cert or equivalent identity proof
- client sends ephemeral public key
- egress sends service cert chain
- egress sends ephemeral public key
- both sides sign or otherwise authenticate the handshake transcript
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

## Slice 7: Encrypted Data Frames

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
type EncryptedStreamReader struct { ... }
type EncryptedStreamWriter struct { ... }
```

Then the existing proxying model can remain conceptually similar, but local TCP is proxied to encrypted stream wrappers instead of directly to the raw QUIC stream.

Open design items:

- exact frame ordering
- whether handshake data is carried in existing response frames or new frame types
- AEAD choice
- key schedule
- nonce construction
- close/error frame semantics
- max encrypted record size
- half-close behavior over encrypted frames

## Slice 8: Require and Enforce E2E Policy

Once encrypted payload frames work, add policy controls.

Possible forward-side config:

```yaml
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: echo
    require_e2e: true
```

Possible service-side behavior:

- if a service has `e2e` configured, require the E2E handshake
- final service authorization uses original client identity from the E2E handshake
- target dialing happens only after successful E2E authentication and authorization

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
