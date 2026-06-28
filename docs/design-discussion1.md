# gorelay Design Discussion Notes

## Project Summary

`gorelay` is intended to be a chainable network relay/proxy for TCP and UDP traffic, written in Go. Relay nodes communicate with each other using QUIC.

Basic topology:

```text
TCP/UDP client -> gorelay client -> gorelay server -> TCP/UDP server
```

Chained topology:

```text
TCP/UDP client -> gorelay client -> gorelay server -> gorelay server -> TCP/UDP server
```

A practical initial limit of 3 hops is acceptable, though the protocol can be designed to support arbitrary route length with a configured maximum.

## Core Requirements

- Written in Go.
- QUIC as the relay-to-relay communication protocol.
- Support TCP and UDP proxying.
- Support multiple simultaneous proxied connections.
- Support relay chaining.
- Client should be able to view a topology map of available/connected relay servers.
- Initial peer connections may be established in either direction.
  - Only one side of a peer relationship needs to expose an open port.
  - Once connected, the relationship should be treated symmetrically regardless of who dialed.
- Each relay/client can be statically configured.
  - Node identifiers.
  - Peer addresses.
  - Ports.
  - Routes.
  - Services.
- Public/private keys and mTLS should be used for node authentication.
- If traffic passes through an intermediary relay, that intermediary must not be able to decrypt the proxied payload.

## Recommended Mental Model

`gorelay` should be designed as a small authenticated QUIC overlay network rather than only a simple proxy.

Each node may act as one or more of:

- local ingress proxy
- relay node
- egress node
- topology participant
- peer connector/listener

A single binary can behave differently based on configuration.

## QUIC Usage

QUIC is a good fit because it provides:

- multiplexed streams
- TLS 1.3 transport security
- low-latency connection setup
- bidirectional streams
- optional datagram support for UDP-like traffic
- user-space Go implementation via `quic-go`

Likely Go library:

```text
github.com/quic-go/quic-go
```

Recommended mapping:

- One QUIC connection per peer pair.
- One long-lived control stream per peer connection.
- One QUIC stream per proxied TCP connection.
- UDP can later use QUIC datagrams or framed UDP-over-streams.

## TCP Forwarding

For TCP, each accepted local TCP connection maps to a QUIC stream.

Example:

```text
local TCP connection -> encrypted gorelay frames -> QUIC stream -> egress TCP connection
```

This should be the first implementation target because it is simpler than UDP.

## UDP Forwarding

UDP can be added later using either:

### Option A: QUIC Datagrams

Pros:

- Natural mapping for UDP packets.
- Avoids stream head-of-line blocking.
- Preserves unreliable datagram-like behavior.

Cons:

- Requires datagram support.
- MTU and packet sizing need careful handling.

### Option B: UDP-over-Stream Framing

Pros:

- Easier to implement and debug.
- Reliable delivery semantics.

Cons:

- Changes UDP semantics.
- May add unwanted ordering and reliability.

Initial recommendation: defer UDP until TCP relay, mTLS, routing, and chaining are stable.

## Peer Connections in Either Direction

A peer relationship should not depend on which side initiated the QUIC connection.

For example, both of these should result in the same logical peer session:

```text
relay-a dials relay-b
relay-b accepts
```

or:

```text
relay-b dials relay-a
relay-a accepts
```

After mTLS authentication, the remote node identity should be extracted from the certificate and registered in the peer manager.

Example session abstraction:

```go
type PeerSession struct {
    RemoteID  NodeID
    Direction SessionDirection
    Conn      quic.Connection
}
```

Duplicate sessions need deterministic handling. If both peers dial each other, choose a stable tie-break rule, such as:

- keep the session initiated by the lexicographically lower node ID; or
- keep the newest session; or
- allow both but mark one as passive.

A deterministic rule is preferred.

## Identity and mTLS

mTLS should authenticate each direct peer relationship.

Each node should have:

- private key
- certificate
- trusted CA bundle
- node ID

The node ID should be bound to the certificate. A SPIFFE-like URI SAN is a good option:

```text
spiffe://gorelay/relay-a
```

mTLS provides:

- transport encryption between adjacent peers
- node authentication
- tamper protection on each direct link
- a basis for authorization policy

However, mTLS alone is not enough to prevent intermediary relays from reading proxied payloads.

## End-to-End Payload Encryption

To prevent intermediary relays from decrypting proxied traffic, use two layers of security:

```text
Application payload encryption  = end-to-end, ingress -> egress
QUIC mTLS                        = hop-by-hop, peer -> peer
```

Layering:

```text
TCP/UDP payload
  encrypted end-to-end by gorelay
    carried inside gorelay relay protocol
      carried inside QUIC stream/datagram
        protected by QUIC TLS/mTLS to the next peer
```

For a path:

```text
client -> relay A -> relay B -> target
```

Security should look like:

```text
client ==mTLS== relay A ==mTLS== relay B
client ==end-to-end encrypted payload== relay B
```

Relay A can authenticate and forward traffic, but cannot decrypt the proxied TCP/UDP bytes.

## Why mTLS Alone Is Insufficient

QUIC mTLS is terminated at each hop.

In this path:

```text
client -> relay A -> relay B
```

The client-to-relay-A QUIC session is encrypted. The relay-A-to-relay-B QUIC session is also encrypted. But relay A terminates the first QUIC session and would see plaintext unless the proxied payload is separately encrypted.

Therefore:

- mTLS protects links.
- end-to-end application encryption protects payloads through intermediaries.

## What Should Be Encrypted End-to-End

At minimum:

- proxied TCP/UDP bytes

Preferably also:

- final destination service
- target host/port
- connection metadata only needed by the egress node

An intermediary should ideally see only:

```text
next_hop
stream_id
message_type
ciphertext
```

The egress node decrypts and sees:

```text
service or destination
payload bytes
```

## Suggested Payload Encryption Design

For each proxied connection, establish an end-to-end encrypted session between the ingress node and the egress node.

Recommended primitives:

- X25519 for key agreement
- HKDF-SHA256 for key derivation
- ChaCha20-Poly1305 or AES-GCM for AEAD encryption
- or HPKE to package this cleanly

HPKE is a strong candidate because it is designed for encrypting to a recipient public key.

Possible Go package:

```text
github.com/cloudflare/circl/hpke
```

or related HPKE-capable libraries.

## HPKE-Based Open Flow

For each new proxied connection:

1. Ingress node identifies the egress node.
2. Ingress node obtains or already knows the egress node public key/certificate.
3. Ingress creates an ephemeral encryption context for the egress public key.
4. Ingress sends an `OPEN` message containing:
   - route/next-hop info visible to relays
   - HPKE encapsulated key
   - encrypted open payload
5. Intermediaries forward the message but cannot decrypt the encrypted open payload.
6. Egress node derives the same encryption context and decrypts the open payload.
7. Data frames are encrypted/decrypted using the established per-connection context.

Visible outer open frame:

```json
{
  "type": "OPEN",
  "stream_id": "abc123",
  "route": ["relay-a", "relay-b"],
  "next_hop": "relay-a",
  "egress_node": "relay-b",
  "hpke_encapsulated_key": "...",
  "encrypted_open_payload": "..."
}
```

Encrypted open payload visible only to the egress:

```json
{
  "service": "postgres-prod",
  "network": "tcp"
}
```

Data frame visible to intermediaries:

```json
{
  "type": "DATA",
  "stream_id": "abc123",
  "seq": 42,
  "ciphertext": "..."
}
```

Plaintext after egress decrypts:

```text
TCP bytes or UDP datagram payload
```

## Framing

Think in terms of encrypted frames rather than raw packets.

For TCP:

- read bytes from the local TCP connection
- split into chunks
- encrypt each chunk into a frame
- send frames over a QUIC stream
- egress decrypts and writes bytes to the target TCP connection

For UDP:

- each UDP datagram can become one encrypted frame
- preserve datagram boundaries
- send using QUIC datagrams or framed QUIC streams

## Relay Protocol Sketch

A control stream can carry messages such as:

- `HELLO`
- `NODE_INFO`
- `PEER_LIST`
- `ROUTE_ADVERTISE`
- `OPEN`
- `CLOSE`
- `ERROR`
- `PING`

Data streams or data frames carry encrypted proxied traffic.

A multi-hop open request can include a route:

```json
{
  "type": "OPEN",
  "stream_id": "abc123",
  "route": ["relay-a", "relay-b"],
  "destination": {
    "service": "postgres-prod"
  }
}
```

For better privacy, the final destination/service should be inside the encrypted open payload, not visible to intermediaries.

## Routing and Chaining

Start with explicit static routes rather than dynamic pathfinding.

Example local forward config:

```yaml
local_forwards:
  - listen: 127.0.0.1:15432
    service: postgres-prod
    route:
      - relay-a
      - relay-b
```

On the egress relay:

```yaml
egress_services:
  - name: postgres-prod
    network: tcp
    target: 10.0.0.5:5432
```

A service-based model is preferred over arbitrary host/port forwarding because it improves:

- policy control
- topology presentation
- route validation
- security boundaries

## Topology Map

The client should be able to see the relay topology.

Initial topology data can come from:

- static config
- active peer sessions
- peer advertisements

Possible topology advertisement:

```json
{
  "node_id": "relay-a",
  "version": 12,
  "timestamp": "2026-06-28T12:00:00Z",
  "listeners": ["relay-a.example.com:4433"],
  "peers": ["relay-b", "relay-c"],
  "services": ["postgres-prod"],
  "signature": "..."
}
```

Initial implementation can keep this simple:

- each node reports direct peers
- client asks a connected node for known topology
- node returns merged/static view

Example CLI output:

```text
gorelay topology

client
  └── relay-a
        └── relay-b
              └── postgres-prod
```

## Security Considerations

Authentication is not authorization. mTLS proves peer identity, but policy should decide what the peer can do.

Consider policies for:

- which nodes may connect
- which nodes may forward traffic
- which services a client may access
- which routes are allowed
- which peers a relay may forward to

Intermediaries will still see metadata, including:

- previous hop
- next hop
- stream IDs
- traffic volume
- timing
- connection duration

End-to-end encryption hides payload and encrypted destination metadata, but it does not hide traffic analysis metadata.

## Suggested Implementation Phases

### MVP 1: TCP, One Hop

- Go
- `quic-go`
- mTLS
- static config
- one QUIC connection per peer
- one QUIC stream per TCP connection
- multiple simultaneous TCP connections

### MVP 2: Direction-Independent Peer Establishment

- peers may dial or listen
- normalize sessions after mTLS identity verification
- handle duplicate sessions deterministically

### MVP 3: Multi-Hop Forwarding

- explicit routes
- max hop count, e.g. 3
- forwarding relay consumes one route hop and forwards to the next

### MVP 4: End-to-End Payload Encryption

- ingress-to-egress encryption
- intermediaries forward opaque ciphertext
- service/destination info encrypted for egress only

### MVP 5: UDP Support

- decide between QUIC datagrams and UDP-over-streams
- preserve datagram boundaries if possible

### MVP 6: Topology View

- expose connected peers
- expose configured routes/services
- add `gorelay topology` command

## Possible Go Package Structure

```text
cmd/gorelay/
  main.go

internal/config/
  config.go

internal/identity/
  certs.go
  mtls.go

internal/quictransport/
  listener.go
  dialer.go
  session.go

internal/protocol/
  messages.go
  codec.go
  control.go

internal/relay/
  node.go
  peer_manager.go
  stream_forwarder.go
  router.go

internal/proxy/
  tcp_listener.go
  udp_listener.go

internal/topology/
  graph.go
  advertise.go

internal/crypto/
  e2e.go
```

## Key Takeaways

- Use QUIC with mTLS for hop-by-hop authenticated transport.
- Use separate end-to-end application-layer encryption for proxied payloads.
- mTLS alone does not protect payloads from intermediary relays.
- Start with TCP, static config, mTLS, and one-hop relay.
- Add bidirectional peer establishment and multi-hop routing next.
- Add end-to-end HPKE/AEAD payload encryption before treating intermediaries as untrusted.
- Add UDP and topology discovery after the core relay model is stable.
