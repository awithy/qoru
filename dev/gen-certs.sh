#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="${1:-dev/certs}"
DAYS="${DAYS:-365}"

mkdir -p "$OUT_DIR"

cat > "$OUT_DIR/ca.cnf" <<'EOF'
[req]
distinguished_name = req_distinguished_name
x509_extensions = v3_ca
prompt = no

[req_distinguished_name]
CN = qoru-dev-ca

[v3_ca]
basicConstraints = critical,CA:TRUE
keyUsage = critical,keyCertSign,cRLSign
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid:always,issuer
EOF

openssl genrsa -out "$OUT_DIR/ca.key" 4096
openssl req -x509 -new -nodes \
  -key "$OUT_DIR/ca.key" \
  -sha256 \
  -days "$DAYS" \
  -out "$OUT_DIR/ca.crt" \
  -config "$OUT_DIR/ca.cnf"

gen_node_cert() {
  local name="$1"
  local san="$2"

  cat > "$OUT_DIR/$name.cnf" <<EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = $name

[v3_req]
basicConstraints = CA:FALSE
keyUsage = critical,digitalSignature,keyEncipherment
extendedKeyUsage = serverAuth,clientAuth
subjectAltName = $san
EOF

  openssl genrsa -out "$OUT_DIR/$name.key" 2048
  openssl req -new \
    -key "$OUT_DIR/$name.key" \
    -out "$OUT_DIR/$name.csr" \
    -config "$OUT_DIR/$name.cnf"
  openssl x509 -req \
    -in "$OUT_DIR/$name.csr" \
    -CA "$OUT_DIR/ca.crt" \
    -CAkey "$OUT_DIR/ca.key" \
    -CAcreateserial \
    -out "$OUT_DIR/$name.crt" \
    -days "$DAYS" \
    -sha256 \
    -extensions v3_req \
    -extfile "$OUT_DIR/$name.cnf"

  rm -f "$OUT_DIR/$name.csr"
}

gen_node_cert "client-1" "URI:spiffe://qoru/node/client-1"
gen_node_cert "server-1" "URI:spiffe://qoru/node/server-1"
gen_node_cert "relay-a" "URI:spiffe://qoru/node/relay-a"
gen_node_cert "relay-b" "URI:spiffe://qoru/node/relay-b"
gen_node_cert "relay-c" "URI:spiffe://qoru/node/relay-c"

rm -f "$OUT_DIR"/*.cnf "$OUT_DIR"/*.srl

printf 'Generated dev certificates in %s\n' "$OUT_DIR"
