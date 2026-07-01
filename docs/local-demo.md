# Local Demo

The simplest demo runs a one-hop TCP proxy locally:

```text
TCP client -> qoru client -> QUIC/mTLS -> qoru server -> TCP echo target
```

Additional smoke tests exercise explicit two-hop and three-hop relay routes, plus E2E policy/encryption behavior.

## Automated end-to-end checks

Run all local smoke tests:

```sh
make demo-all
```

Or run individual smoke tests:

```sh
make demo-e2e
make demo-multihop
make demo-threehop
make demo-e2e-auto-direct
make demo-e2e-encrypted
```

`demo-e2e` starts an echo target, qoru server, and qoru client; sends a test payload through the local client listener; verifies the echoed response; then cleans up. The generated temporary server config exposes an `echo` service for `client-1`.

`demo-e2e-auto-direct` configures an E2E-capable service and a client forward with `e2e: auto` on a direct one-hop path. It verifies the payload succeeds and that no E2E handshake is run for the direct path.

`demo-e2e-encrypted` configures an E2E-capable service behind a two-hop route and a client forward with `e2e: auto`. It verifies the payload succeeds and that both ingress and egress log a completed E2E handshake.

By default, the scripts choose free local ports. The one-hop script addresses can be overridden with `QORU_DEMO_SERVER_ADDR`, `QORU_DEMO_CLIENT_ADDR`, and `QORU_DEMO_TARGET_ADDR`.

## Manual demo

## 1. Generate dev certificates

```sh
make gen-dev-certs
```

## 2. Start a local TCP echo target

In terminal 1:

```sh
go run ./dev/echo-server -listen 127.0.0.1:9000
```

## 3. Start qoru server

In terminal 2:

```sh
go run ./cmd/qoru server -c examples/config/server.yaml
```

Expected log:

```text
msg="server listening" node_id=server-1 addr=127.0.0.1:4433
```

## 4. Start qoru client

In terminal 3:

```sh
go run ./cmd/qoru client -c examples/config/client.yaml
```

The example client config listens on:

```text
127.0.0.1:15432
```

and requests the `echo` service from `server-1`. The server maps that service to the local echo target:

```text
127.0.0.1:9000
```

## 5. Connect through the proxy

In terminal 4:

```sh
nc 127.0.0.1 15432
```

Type text and press Enter. The echo server should send the same text back through qoru.

Example:

```text
hello
hello
```

## Notes

- The client and server use QUIC with mTLS.
- The dev certs are generated under `dev/certs/` and are ignored by git.
- The current implementation supports one-hop and explicit-route multi-hop TCP forwarding.
- Server-side access policy is service/peer based today. The automated demo exposes the `echo` service only to `client-1`.
