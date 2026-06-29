#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

free_tcp_addr() {
  python3 - <<'PY'
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.bind(("127.0.0.1", 0))
print(f"127.0.0.1:{s.getsockname()[1]}")
s.close()
PY
}

free_udp_addr() {
  python3 - <<'PY'
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.bind(("127.0.0.1", 0))
print(f"127.0.0.1:{s.getsockname()[1]}")
s.close()
PY
}

SERVER_ADDR="${QORU_DEMO_SERVER_ADDR:-$(free_udp_addr)}"
CLIENT_ADDR="${QORU_DEMO_CLIENT_ADDR:-$(free_tcp_addr)}"
TARGET_ADDR="${QORU_DEMO_TARGET_ADDR:-$(free_tcp_addr)}"
MESSAGE="${QORU_DEMO_MESSAGE:-qoru-e2e-ping}"

tmpdir="$(mktemp -d)"
pids=()
cleanup() {
  for pid in "${pids[@]:-}"; do
    if kill -0 "$pid" >/dev/null 2>&1; then
      kill "$pid" >/dev/null 2>&1 || true
    fi
  done
  for pid in "${pids[@]:-}"; do
    wait "$pid" >/dev/null 2>&1 || true
  done
  rm -rf "$tmpdir"
}
trap cleanup EXIT

wait_for_tcp() {
  local addr="$1"
  local host="${addr%:*}"
  local port="${addr##*:}"
  local deadline=$((SECONDS + 10))
  while (( SECONDS < deadline )); do
    if timeout 1 bash -c "</dev/tcp/$host/$port" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.1
  done
  echo "timed out waiting for $addr" >&2
  return 1
}

wait_for_log() {
  local file="$1"
  local pattern="$2"
  local deadline=$((SECONDS + 10))
  while (( SECONDS < deadline )); do
    if grep -q "$pattern" "$file" 2>/dev/null; then
      return 0
    fi
    sleep 0.1
  done
  echo "timed out waiting for log pattern $pattern in $file" >&2
  cat "$file" >&2 || true
  return 1
}

send_tcp() {
  local addr="$1"
  local host="${addr%:*}"
  local port="${addr##*:}"
  if command -v nc >/dev/null 2>&1; then
    printf '%s' "$MESSAGE" | nc -w 3 "$host" "$port"
  else
    python3 - "$host" "$port" "$MESSAGE" <<'PY'
import socket, sys
host, port, message = sys.argv[1], int(sys.argv[2]), sys.argv[3].encode()
with socket.create_connection((host, port), timeout=3) as s:
    s.sendall(message)
    data = b''
    while len(data) < len(message):
        chunk = s.recv(len(message) - len(data))
        if not chunk:
            break
        data += chunk
sys.stdout.buffer.write(data)
PY
  fi
}

cat >"$tmpdir/server.yaml" <<EOF
node_id: server-1
mode: server
identity:
  cert: ./dev/certs/server-1.crt
  key: ./dev/certs/server-1.key
  ca: ./dev/certs/ca.crt
listen: $SERVER_ADDR
allowed_targets:
  - protocol: tcp
    address: $TARGET_ADDR
EOF

cat >"$tmpdir/client.yaml" <<EOF
node_id: client-1
mode: client
identity:
  cert: ./dev/certs/client-1.crt
  key: ./dev/certs/client-1.key
  ca: ./dev/certs/ca.crt
server:
  id: server-1
  address: $SERVER_ADDR
forwards:
  - protocol: tcp
    listen: $CLIENT_ADDR
    target: $TARGET_ADDR
EOF

echo "==> generating dev certs"
make gen-dev-certs >/dev/null

echo "==> starting echo target on $TARGET_ADDR"
go run ./dev/echo-server -listen "$TARGET_ADDR" >"$tmpdir/echo.log" 2>&1 &
pids+=("$!")
wait_for_tcp "$TARGET_ADDR"

echo "==> starting qoru server on $SERVER_ADDR"
go run ./cmd/qoru server -c "$tmpdir/server.yaml" >"$tmpdir/server.log" 2>&1 &
pids+=("$!")
wait_for_log "$tmpdir/server.log" "server listening"

echo "==> starting qoru client on $CLIENT_ADDR"
go run ./cmd/qoru client -c "$tmpdir/client.yaml" >"$tmpdir/client.log" 2>&1 &
pids+=("$!")
wait_for_tcp "$CLIENT_ADDR"

echo "==> sending test payload through qoru"
response="$(send_tcp "$CLIENT_ADDR")"
if [[ "$response" != "$MESSAGE" ]]; then
  echo "expected response $MESSAGE, got $response" >&2
  echo "--- echo.log ---" >&2; cat "$tmpdir/echo.log" >&2 || true
  echo "--- server.log ---" >&2; cat "$tmpdir/server.log" >&2 || true
  echo "--- client.log ---" >&2; cat "$tmpdir/client.log" >&2 || true
  exit 1
fi

echo "ok: received echoed payload through qoru: $response"
