# Local Demo

This demo runs a one-hop TCP proxy locally:

```text
TCP client -> qoru client -> QUIC/mTLS -> qoru server -> TCP echo target
```

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

and forwards to the echo target through the qoru server:

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
- The current implementation is one-hop only.
- Server-side access policy is not implemented yet; authenticated clients can request arbitrary TCP targets.
