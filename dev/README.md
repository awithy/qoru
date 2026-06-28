# Development certificates

Generate local mTLS certificates with:

```sh
./dev/gen-certs.sh
```

This creates:

```text
dev/certs/ca.crt
dev/certs/ca.key
dev/certs/client-1.crt
dev/certs/client-1.key
dev/certs/server-1.crt
dev/certs/server-1.key
```

These files are for local development only and are ignored by git.
