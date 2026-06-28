# gorelay

`gorelay` is an experimental chainable network relay/proxy for TCP and UDP connections, written in Go.

The goal is to create a small authenticated QUIC-based relay overlay where clients and relay nodes can forward traffic across one or more hops while preserving end-to-end payload confidentiality from intermediary relays.

## Concept

Basic one-hop topology:

```text
TCP/UDP client -> gorelay client -> gorelay server -> TCP/UDP server
```

Chained topology:

```text
TCP/UDP client -> gorelay client -> gorelay server -> gorelay server -> TCP/UDP server
```

`gorelay` nodes communicate with each other over QUIC. Each peer connection is authenticated with mTLS, and proxied payloads can be encrypted end-to-end between the ingress and egress nodes so intermediary relays cannot inspect the traffic they forward.

## Goals

- Written in Go.
- Use QUIC for relay-to-relay communication.
- Support TCP proxying.
- Support UDP proxying eventually.
- Support multiple simultaneous connections.
- Support chained relay paths, initially with a small maximum hop count.
- Allow peer connections to be established in either direction.
- Use public/private keys and mTLS for node identity.
- Provide a client-visible topology map of relay nodes.
- Support static configuration for nodes, peers, routes, services, and ports.
- Encrypt proxied payloads end-to-end so intermediary relays cannot decrypt them.

## Non-Goals for the Initial Version

The first version should stay intentionally small. These features can come later:

- fully dynamic routing
- NAT traversal
- automatic peer discovery
- complex gossip protocols
- onion-routing-style anonymity
- production-grade certificate rotation
- advanced UDP behavior

## Security Model

`gorelay` uses two security layers:

```text
Application payload encryption  = end-to-end, ingress -> egress
QUIC mTLS                        = hop-by-hop, peer -> peer
```

For a path like:

```text
client -> relay-a -> relay-b -> target
```

mTLS protects each direct QUIC connection:

```text
client ==mTLS== relay-a ==mTLS== relay-b
```

End-to-end payload encryption protects the proxied traffic:

```text
client ==encrypted payload== relay-b
```

This means `relay-a` can authenticate peers and forward traffic, but cannot decrypt the proxied TCP/UDP payload intended for `relay-b`.

Intermediaries may still see routing metadata such as previous hop, next hop, stream ID, traffic volume, and timing.

## Design Direction

The intended architecture is a small QUIC overlay network.

Each `gorelay` process may act as one or more of:

- local ingress proxy
- relay node
- egress node
- peer listener
- peer dialer
- topology participant

The same binary should be configurable for client-like or server-like behavior.

## Suggested Implementation Phases

### Phase 1: TCP, One Hop

- Static configuration.
- QUIC peer connection using `quic-go`.
- mTLS authentication.
- One QUIC stream per TCP connection.
- Multiple simultaneous TCP connections.

### Phase 2: Direction-Independent Peer Sessions

- Allow either side to dial or listen.
- Normalize peer sessions after mTLS identity verification.
- Handle duplicate sessions deterministically.

### Phase 3: Multi-Hop Forwarding

- Explicit configured routes.
- Forward traffic across relay chains.
- Enforce a configured maximum hop count.

### Phase 4: End-to-End Payload Encryption

- Encrypt proxied payloads between ingress and egress nodes.
- Keep final service/destination metadata encrypted from intermediaries.
- Consider HPKE plus AEAD framing.

### Phase 5: UDP Support

- Support UDP datagrams using QUIC datagrams or UDP-over-stream framing.
- Preserve datagram boundaries where possible.

### Phase 6: Topology View

- Track configured and connected peers.
- Expose topology via CLI/API.
- Add commands such as `gorelay topology`.

## Possible Package Layout

```text
cmd/gorelay/
  main.go

internal/config/
internal/identity/
internal/quictransport/
internal/protocol/
internal/relay/
internal/proxy/
internal/topology/
internal/crypto/
```

## Example Future Configuration

```yaml
node_id: client-1

identity:
  cert: ./client-1.crt
  key: ./client-1.key
  ca: ./ca.crt

peers:
  - id: relay-a
    mode: connect
    address: relay-a.example.com:4433

local_forwards:
  - listen: 127.0.0.1:15432
    service: postgres-prod
    route:
      - relay-a
      - relay-b
```

Example egress service configuration:

```yaml
node_id: relay-b

egress_services:
  - name: postgres-prod
    network: tcp
    target: 10.0.0.5:5432
```

## Documentation

Design notes from the initial discussion are available in:

```text
docs/design-discussion1.md
```

## Status

Early design stage. No implementation yet.
