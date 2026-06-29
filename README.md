# qoru

`qoru` is an experimental QUIC-based network relay/proxy, written in Go.

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
- Custom binary control protocol.
- One QUIC connection shared by client-side TCP forwards.
- One QUIC stream per proxied TCP connection.
- Multiple local TCP forwards.
- Server-side TCP target dialing and byte proxying.
- Optional server-side TCP target allowlist.
- Development certificate generation.
- Local echo-server demo.

## Quick Start: Local Demo

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

server:
  id: server-1
  address: 127.0.0.1:4433

forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    target: 127.0.0.1:9000
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

# Optional. If omitted or empty, any syntactically valid TCP target is allowed.
allowed_targets:
  - protocol: tcp
    address: 127.0.0.1:9000
```

## Security Model

`qoru` is intended to use two security layers:

```text
Application payload encryption  = end-to-end, ingress -> egress
QUIC mTLS                        = hop-by-hop, peer -> peer
```

The current implementation has QUIC/mTLS hop-by-hop encryption and authentication. End-to-end application payload encryption for multi-hop relay paths is not implemented yet.

For the current one-hop path:

```text
qoru client ==QUIC/mTLS== qoru server
```

mTLS uses certificates signed by the configured CA. The system trust store is not used.

## Roadmap

Near-term:

- Improve active connection shutdown behavior.
- Add clearer target dial failure behavior for local TCP clients.
- Add per-peer/per-client target access policy.
- Add more robust reconnect behavior for client/server QUIC sessions.

Longer-term:

- Multi-hop forwarding.
- End-to-end encrypted payload frames.
- UDP support.
- Topology/status commands.
- Direction-independent peer sessions.

## Documentation

```text
docs/design.md
docs/local-demo.md
docs/handoff.md
docs/design-discussion1.md
```

## Status

Experimental. The current code supports a basic local one-hop TCP proxy over QUIC/mTLS. APIs, config, and protocol details are expected to change.
