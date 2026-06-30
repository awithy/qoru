#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

free_tcp_addr() { python3 - <<'PY'
import socket
s=socket.socket(socket.AF_INET,socket.SOCK_STREAM); s.bind(("127.0.0.1",0)); print(f"127.0.0.1:{s.getsockname()[1]}"); s.close()
PY
}
free_udp_addr() { python3 - <<'PY'
import socket
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.bind(("127.0.0.1",0)); print(f"127.0.0.1:{s.getsockname()[1]}"); s.close()
PY
}

RELAY_A_ADDR="${QORU_DEMO_RELAY_A_ADDR:-$(free_udp_addr)}"
RELAY_B_ADDR="${QORU_DEMO_RELAY_B_ADDR:-$(free_udp_addr)}"
RELAY_C_ADDR="${QORU_DEMO_RELAY_C_ADDR:-$(free_udp_addr)}"
CLIENT_ADDR="${QORU_DEMO_CLIENT_ADDR:-$(free_tcp_addr)}"
TARGET_ADDR="${QORU_DEMO_TARGET_ADDR:-$(free_tcp_addr)}"
MESSAGE="${QORU_DEMO_MESSAGE:-qoru-threehop-ping}"

tmpdir="$(mktemp -d)"
pids=()
cleanup(){ for pid in "${pids[@]:-}"; do kill "$pid" >/dev/null 2>&1 || true; done; for pid in "${pids[@]:-}"; do wait "$pid" >/dev/null 2>&1 || true; done; rm -rf "$tmpdir"; }
trap cleanup EXIT

wait_for_tcp(){ local addr="$1"; local host="${addr%:*}"; local port="${addr##*:}"; local deadline=$((SECONDS+10)); while (( SECONDS < deadline )); do timeout 1 bash -c "</dev/tcp/$host/$port" >/dev/null 2>&1 && return 0; sleep 0.1; done; echo "timed out waiting for $addr" >&2; return 1; }
wait_for_log(){ local file="$1"; local pattern="$2"; local deadline=$((SECONDS+10)); while (( SECONDS < deadline )); do grep -q "$pattern" "$file" 2>/dev/null && return 0; sleep 0.1; done; echo "timed out waiting for log pattern $pattern in $file" >&2; cat "$file" >&2 || true; return 1; }
send_tcp(){ local addr="$1"; local host="${addr%:*}"; local port="${addr##*:}"; if command -v nc >/dev/null 2>&1; then printf '%s' "$MESSAGE" | nc -w 3 "$host" "$port"; else python3 - "$host" "$port" "$MESSAGE" <<'PY'
import socket, sys
host, port, message = sys.argv[1], int(sys.argv[2]), sys.argv[3].encode()
with socket.create_connection((host, port), timeout=3) as s:
    s.sendall(message); data=b''
    while len(data)<len(message):
        chunk=s.recv(len(message)-len(data))
        if not chunk: break
        data+=chunk
sys.stdout.buffer.write(data)
PY
fi; }

cat >"$tmpdir/relay-c.yaml" <<EOF
node_id: relay-c
mode: server
identity:
  cert: ./dev/certs/relay-c.crt
  key: ./dev/certs/relay-c.key
  ca: ./dev/certs/ca.crt
listen: $RELAY_C_ADDR
services:
  - name: echo
    protocol: tcp
    target: $TARGET_ADDR
    peers:
      - relay-b
EOF
cat >"$tmpdir/relay-b.yaml" <<EOF
node_id: relay-b
mode: server
identity:
  cert: ./dev/certs/relay-b.crt
  key: ./dev/certs/relay-b.key
  ca: ./dev/certs/ca.crt
listen: $RELAY_B_ADDR
peers:
  - id: relay-c
    address: $RELAY_C_ADDR
    dial: true
EOF
cat >"$tmpdir/relay-a.yaml" <<EOF
node_id: relay-a
mode: server
identity:
  cert: ./dev/certs/relay-a.crt
  key: ./dev/certs/relay-a.key
  ca: ./dev/certs/ca.crt
listen: $RELAY_A_ADDR
peers:
  - id: relay-b
    address: $RELAY_B_ADDR
    dial: true
EOF
cat >"$tmpdir/client.yaml" <<EOF
node_id: client-1
mode: client
identity:
  cert: ./dev/certs/client-1.crt
  key: ./dev/certs/client-1.key
  ca: ./dev/certs/ca.crt
servers:
  - id: relay-a
    address: $RELAY_A_ADDR
forwards:
  - protocol: tcp
    listen: $CLIENT_ADDR
    service: echo
    egress: relay-c
    route: [relay-a, relay-b, relay-c]
EOF

echo "==> generating dev certs"; make gen-dev-certs >/dev/null

echo "==> starting echo target on $TARGET_ADDR"; go run ./dev/echo-server -listen "$TARGET_ADDR" >"$tmpdir/echo.log" 2>&1 & pids+=("$!"); wait_for_tcp "$TARGET_ADDR"
echo "==> starting relay-c on $RELAY_C_ADDR"; go run ./cmd/qoru server -c "$tmpdir/relay-c.yaml" >"$tmpdir/relay-c.log" 2>&1 & pids+=("$!"); wait_for_log "$tmpdir/relay-c.log" "server listening"
echo "==> starting relay-b on $RELAY_B_ADDR"; go run ./cmd/qoru server -c "$tmpdir/relay-b.yaml" >"$tmpdir/relay-b.log" 2>&1 & pids+=("$!"); wait_for_log "$tmpdir/relay-b.log" "peer connected"
echo "==> starting relay-a on $RELAY_A_ADDR"; go run ./cmd/qoru server -c "$tmpdir/relay-a.yaml" >"$tmpdir/relay-a.log" 2>&1 & pids+=("$!"); wait_for_log "$tmpdir/relay-a.log" "peer connected"
echo "==> starting qoru client on $CLIENT_ADDR"; go run ./cmd/qoru client -c "$tmpdir/client.yaml" >"$tmpdir/client.log" 2>&1 & pids+=("$!"); wait_for_tcp "$CLIENT_ADDR"

echo "==> sending test payload through qoru three-hop route"
response="$(send_tcp "$CLIENT_ADDR")"
if [[ "$response" != "$MESSAGE" ]]; then
  echo "expected response $MESSAGE, got $response" >&2
  for f in echo relay-a relay-b relay-c client; do echo "--- $f.log ---" >&2; cat "$tmpdir/$f.log" >&2 || true; done
  exit 1
fi

echo "ok: received echoed payload through qoru three-hop route: $response"
