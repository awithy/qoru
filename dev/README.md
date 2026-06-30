# Development certificates

Generate local mTLS certificates with:

```sh
./dev/gen-certs.sh
```

This creates node mTLS certificates plus development service identity certificates:

```text
dev/certs/ca.crt
dev/certs/ca.key
dev/certs/service-ca.crt
dev/certs/service-ca.key
dev/certs/client-1.crt
dev/certs/client-1.key
dev/certs/server-1.crt
dev/certs/server-1.key
dev/certs/relay-a.crt
dev/certs/relay-a.key
dev/certs/relay-b.crt
dev/certs/relay-b.key
dev/certs/relay-c.crt
dev/certs/relay-c.key
dev/certs/relay-b-echo.crt
dev/certs/relay-b-echo.key
dev/certs/relay-c-echo.crt
dev/certs/relay-c-echo.key
```

These files are for local development only and are ignored by git.
