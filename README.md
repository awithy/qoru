# qoru

`qoru` is an experimental QUIC-based network relay/proxy, written in Go.  The name is not meaningful.

The long-term goal is to create a small authenticated relay overlay where clients and relay nodes can forward traffic across one or more hops while preserving end-to-end payload confidentiality from intermediary relays.

The current implementation supports a basic one-hop TCP proxy:

```text
TCP client -> qoru client -> QUIC/mTLS -> qoru server -> TCP target
```

## Current Features

- Go CLI using Cobra.
- YAML configuration.
- QUIC transport using `quic-go`.
- mTLS peer authentication with a configured private CA.
- Custom binary control protocol with machine-readable connect response codes.
- One reconnecting upstream QUIC connection per configured client-side server.
- Multiple direct upstream servers selected by forward `egress`.
- On-demand upstream reconnect for new local TCP connections after a QUIC connection loss.
- One QUIC stream per proxied TCP connection.
- Multiple local TCP forwards.
- Named TCP services on the server.
- Per-service peer authorization.
- Optional one-hop egress selection.
- Explicit-route multi-hop TCP forwarding using configured next-hop servers.
- Server-side TCP target dialing and byte proxying.
- SPIFFE-style URI SAN node identities in mTLS certificates.
- Development certificate generation.
- Local echo-server demo and automated e2e smoke test.

## Quick Start: Local Demo

Run the automated local smoke test:

```sh
make demo-e2e
```

Or run the demo manually.

Generate development certificates:

```sh
make gen-dev-certs
```

Start a local TCP echo target:

```sh
go run ./dev/echo-server -listen 127.0.0.1:9000
```

In another terminal, start the qoru server:

```sh
go run ./cmd/qoru server -c examples/config/server.yaml
```

In another terminal, start the qoru client:

```sh
go run ./cmd/qoru client -c examples/config/client.yaml
```

Then connect through the local qoru client listener:

```sh
nc 127.0.0.1 15432
```

Text typed into `nc` should be echoed back through:

```text
nc -> qoru client -> QUIC/mTLS -> qoru server -> echo server
```

See `docs/local-demo.md` for more details.

## Build and Test

```sh
make test
make build
```

The binary is written to:

```text
build/qoru
```

## CLI

```sh
qoru client -c examples/config/client.yaml
qoru server -c examples/config/server.yaml
qoru print-config -c examples/config/client.yaml
```

If `--config` is omitted, qoru checks:

```text
./qoru.yaml
./qoru.yml
/etc/qoru/config.yaml
/etc/qoru/config.yml
```

## Example Configuration

Client:

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

A forward may also include an explicit `route`. The first hop must be a configured direct upstream server, and the final hop is the egress node:

```yaml
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    service: echo
    egress: relay-b
    route:
      - relay-a
      - relay-b
```

Explicit multi-hop routing currently uses hop-by-hop QUIC/mTLS. End-to-end payload encryption through intermediary relays is not implemented yet.

A client can configure multiple direct upstream servers. In that case each forward must set `egress` to a configured server ID:

```yaml
servers:
  - id: server-1
    address: 127.0.0.1:4433
  - id: server-2
    address: 127.0.0.1:4434
```

Server:

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

## Security Model

`qoru` is intended to use two security layers:

```text
Application payload encryption  = end-to-end, ingress -> egress
QUIC mTLS                        = hop-by-hop, peer -> peer
```

The current implementation has QUIC/mTLS hop-by-hop encryption and authentication. End-to-end application payload encryption for multi-hop relay paths is not implemented yet.

If the client-side upstream QUIC connection is lost, active proxied TCP connections on that connection are closed. The qoru client keeps its local listeners running and reconnects on demand for later local TCP connections. Failed reconnect dials use exponential backoff: `500ms`, `1s`, `2s`, `4s`, `8s`, `16s`, capped at `16s`. During backoff, new local TCP connections fail fast without qoru writing diagnostic bytes into the TCP stream.

For the current one-hop path:

```text
qoru client ==QUIC/mTLS== qoru server
```

mTLS uses certificates signed by the configured CA. The system trust store is not used. qoru node identity is taken from SPIFFE-style URI SANs such as:

```text
spiffe://qoru/node/client-1
spiffe://qoru/node/server-1
```

## Roadmap

Near-term:

- Improve active connection shutdown behavior.
- Improve service dial failure behavior for local TCP clients.
- Add better reconnect observability and clearer server-side session handling.
- Add automated explicit-route multi-hop smoke testing and demo config.
- Add richer service selection semantics for future multi-egress/load-balanced service routing.

Longer-term:

- End-to-end encrypted payload frames.
- UDP support.
- Topology/status commands.
- Direction-independent peer sessions.

## Documentation

```text
docs/design.md
docs/local-demo.md
docs/design-discussion1.md
```

## Status

Experimental. The current code supports a basic local one-hop TCP proxy over QUIC/mTLS. APIs, config, and protocol details are expected to change.
