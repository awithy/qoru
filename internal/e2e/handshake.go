package e2e

import (
	"bytes"
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/awithy/qoru/internal/identity"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/awithy/qoru/internal/requestid"
)

const (
	clientHelloDomain    = "qoru-e2e-client-hello-v1"
	serverHelloDomain    = "qoru-e2e-server-hello-v1"
	clientFinishedDomain = "qoru-e2e-client-finished-v1"
	transcriptDomain     = "qoru-e2e-transcript-v1"
	trafficKeysInfo      = "qoru-e2e-traffic-keys-v1"
	trafficKeySize       = 32
)

// Context is the per-connection routing context bound into the E2E handshake.
type Context struct {
	RequestID string
	Service   string
	Egress    string
	Route     []string
}

type EphemeralKeyPair struct {
	Private *ecdh.PrivateKey
	Public  []byte
}

type TrafficKeys struct {
	ClientToServer []byte
	ServerToClient []byte
}

func GenerateEphemeralKey(rng io.Reader) (EphemeralKeyPair, error) {
	if rng == nil {
		rng = rand.Reader
	}
	priv, err := ecdh.X25519().GenerateKey(rng)
	if err != nil {
		return EphemeralKeyPair{}, fmt.Errorf("generate e2e ephemeral key: %w", err)
	}
	return EphemeralKeyPair{Private: priv, Public: append([]byte(nil), priv.PublicKey().Bytes()...)}, nil
}

func SharedSecret(private *ecdh.PrivateKey, peerPublic []byte) ([]byte, error) {
	if private == nil {
		return nil, fmt.Errorf("private ephemeral key is required")
	}
	peer, err := ecdh.X25519().NewPublicKey(peerPublic)
	if err != nil {
		return nil, fmt.Errorf("parse peer ephemeral public key: %w", err)
	}
	secret, err := private.ECDH(peer)
	if err != nil {
		return nil, fmt.Errorf("derive shared secret: %w", err)
	}
	return secret, nil
}

func NewClientHello(ctx Context, cert tls.Certificate, clientEphemeral []byte) (protocol.E2EClientHello, error) {
	if len(cert.Certificate) == 0 {
		return protocol.E2EClientHello{}, fmt.Errorf("client certificate chain is required")
	}
	digest, err := clientHelloDigest(ctx, clientEphemeral)
	if err != nil {
		return protocol.E2EClientHello{}, err
	}
	sig, err := signDigest(cert, digest)
	if err != nil {
		return protocol.E2EClientHello{}, fmt.Errorf("sign client hello: %w", err)
	}
	return protocol.E2EClientHello{ClientCertChain: cloneChain(cert.Certificate), EphemeralPublicKey: append([]byte(nil), clientEphemeral...), Signature: sig}, nil
}

func VerifyClientHello(ctx Context, hello protocol.E2EClientHello, roots *x509.CertPool) (string, *x509.Certificate, error) {
	leaf, clientID, err := verifyClientCertificate(hello.ClientCertChain, roots)
	if err != nil {
		return "", nil, err
	}
	digest, err := clientHelloDigest(ctx, hello.EphemeralPublicKey)
	if err != nil {
		return "", nil, err
	}
	if err := verifySignature(leaf, digest, hello.Signature); err != nil {
		return "", nil, fmt.Errorf("verify client hello signature: %w", err)
	}
	return clientID, leaf, nil
}

func NewServerHello(ctx Context, cert tls.Certificate, clientEphemeral, serverEphemeral []byte) (protocol.E2EServerHello, error) {
	if len(cert.Certificate) == 0 {
		return protocol.E2EServerHello{}, fmt.Errorf("service certificate chain is required")
	}
	digest, err := serverHelloDigest(ctx, clientEphemeral, serverEphemeral)
	if err != nil {
		return protocol.E2EServerHello{}, err
	}
	sig, err := signDigest(cert, digest)
	if err != nil {
		return protocol.E2EServerHello{}, fmt.Errorf("sign server hello: %w", err)
	}
	return protocol.E2EServerHello{ServiceCertChain: cloneChain(cert.Certificate), EphemeralPublicKey: append([]byte(nil), serverEphemeral...), Signature: sig}, nil
}

func VerifyServerHello(ctx Context, hello protocol.E2EServerHello, roots *x509.CertPool, clientEphemeral []byte) (*x509.Certificate, error) {
	if err := identity.VerifyServiceCertificate(hello.ServiceCertChain, roots, ctx.Service); err != nil {
		return nil, err
	}
	leaf, err := parseLeafCertificate(hello.ServiceCertChain)
	if err != nil {
		return nil, err
	}
	digest, err := serverHelloDigest(ctx, clientEphemeral, hello.EphemeralPublicKey)
	if err != nil {
		return nil, err
	}
	if err := verifySignature(leaf, digest, hello.Signature); err != nil {
		return nil, fmt.Errorf("verify server hello signature: %w", err)
	}
	return leaf, nil
}

func NewClientFinished(ctx Context, cert tls.Certificate, clientEphemeral, serverEphemeral []byte) (protocol.E2EClientFinished, error) {
	digest, err := clientFinishedDigest(ctx, clientEphemeral, serverEphemeral)
	if err != nil {
		return protocol.E2EClientFinished{}, err
	}
	sig, err := signDigest(cert, digest)
	if err != nil {
		return protocol.E2EClientFinished{}, fmt.Errorf("sign client finished: %w", err)
	}
	return protocol.E2EClientFinished{Signature: sig}, nil
}

func VerifyClientFinished(ctx Context, finished protocol.E2EClientFinished, clientLeaf *x509.Certificate, clientEphemeral, serverEphemeral []byte) error {
	digest, err := clientFinishedDigest(ctx, clientEphemeral, serverEphemeral)
	if err != nil {
		return err
	}
	if err := verifySignature(clientLeaf, digest, finished.Signature); err != nil {
		return fmt.Errorf("verify client finished signature: %w", err)
	}
	return nil
}

func TranscriptHash(ctx Context, clientEphemeral, serverEphemeral []byte) ([]byte, error) {
	return digestTranscript(transcriptDomain, ctx, clientEphemeral, serverEphemeral)
}

func DeriveTrafficKeys(sharedSecret, transcriptHash []byte) (TrafficKeys, error) {
	if len(sharedSecret) == 0 {
		return TrafficKeys{}, fmt.Errorf("shared secret is required")
	}
	if len(transcriptHash) == 0 {
		return TrafficKeys{}, fmt.Errorf("transcript hash is required")
	}
	keyMaterial, err := hkdf.Key(sha256.New, sharedSecret, transcriptHash, trafficKeysInfo, trafficKeySize*2)
	if err != nil {
		return TrafficKeys{}, fmt.Errorf("derive traffic keys: %w", err)
	}
	return TrafficKeys{
		ClientToServer: append([]byte(nil), keyMaterial[:trafficKeySize]...),
		ServerToClient: append([]byte(nil), keyMaterial[trafficKeySize:]...),
	}, nil
}

func clientHelloDigest(ctx Context, clientEphemeral []byte) ([]byte, error) {
	return digestTranscript(clientHelloDomain, ctx, clientEphemeral, nil)
}

func serverHelloDigest(ctx Context, clientEphemeral, serverEphemeral []byte) ([]byte, error) {
	return digestTranscript(serverHelloDomain, ctx, clientEphemeral, serverEphemeral)
}

func clientFinishedDigest(ctx Context, clientEphemeral, serverEphemeral []byte) ([]byte, error) {
	return digestTranscript(clientFinishedDomain, ctx, clientEphemeral, serverEphemeral)
}

func digestTranscript(domain string, ctx Context, clientEphemeral, serverEphemeral []byte) ([]byte, error) {
	payload, err := marshalTranscript(domain, ctx, clientEphemeral, serverEphemeral)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(payload)
	return digest[:], nil
}

func marshalTranscript(domain string, ctx Context, clientEphemeral, serverEphemeral []byte) ([]byte, error) {
	if domain == "" {
		return nil, fmt.Errorf("transcript domain is required")
	}
	if _, err := requestid.ParseBytes(ctx.RequestID); err != nil {
		return nil, fmt.Errorf("request_id must be a valid UUIDv7: %w", err)
	}
	if ctx.Service == "" {
		return nil, fmt.Errorf("service is required")
	}
	if len(ctx.Service) > protocol.MaxTargetLength {
		return nil, fmt.Errorf("service too long: %d > %d", len(ctx.Service), protocol.MaxTargetLength)
	}
	if len(ctx.Egress) > protocol.MaxTargetLength {
		return nil, fmt.Errorf("egress too long: %d > %d", len(ctx.Egress), protocol.MaxTargetLength)
	}
	if len(ctx.Route) > 255 {
		return nil, fmt.Errorf("route too long: %d > %d", len(ctx.Route), 255)
	}
	for i, hop := range ctx.Route {
		if hop == "" {
			return nil, fmt.Errorf("route[%d] is required", i)
		}
		if len(hop) > protocol.MaxTargetLength {
			return nil, fmt.Errorf("route[%d] too long: %d > %d", i, len(hop), protocol.MaxTargetLength)
		}
	}
	if len(clientEphemeral) == 0 {
		return nil, fmt.Errorf("client ephemeral public key is required")
	}
	if len(clientEphemeral) > protocol.MaxE2EEphemeralKeyLength {
		return nil, fmt.Errorf("client ephemeral public key too long: %d > %d", len(clientEphemeral), protocol.MaxE2EEphemeralKeyLength)
	}
	if _, err := ecdh.X25519().NewPublicKey(clientEphemeral); err != nil {
		return nil, fmt.Errorf("parse client ephemeral public key: %w", err)
	}
	if serverEphemeral != nil {
		if len(serverEphemeral) == 0 {
			return nil, fmt.Errorf("server ephemeral public key is required")
		}
		if len(serverEphemeral) > protocol.MaxE2EEphemeralKeyLength {
			return nil, fmt.Errorf("server ephemeral public key too long: %d > %d", len(serverEphemeral), protocol.MaxE2EEphemeralKeyLength)
		}
		if _, err := ecdh.X25519().NewPublicKey(serverEphemeral); err != nil {
			return nil, fmt.Errorf("parse server ephemeral public key: %w", err)
		}
	}

	var buf bytes.Buffer
	writeString(&buf, domain)
	writeString(&buf, ctx.RequestID)
	writeString(&buf, ctx.Service)
	writeString(&buf, ctx.Egress)
	buf.WriteByte(byte(len(ctx.Route)))
	for _, hop := range ctx.Route {
		writeString(&buf, hop)
	}
	writeBytes(&buf, clientEphemeral)
	if serverEphemeral == nil {
		buf.WriteByte(0)
	} else {
		buf.WriteByte(1)
		writeBytes(&buf, serverEphemeral)
	}
	return buf.Bytes(), nil
}

func signDigest(cert tls.Certificate, digest []byte) ([]byte, error) {
	if len(cert.Certificate) == 0 {
		return nil, fmt.Errorf("certificate chain is required")
	}
	signer, ok := cert.PrivateKey.(crypto.Signer)
	if !ok || signer == nil {
		return nil, fmt.Errorf("certificate private key does not implement crypto.Signer")
	}
	if _, err := parseLeafCertificate(cert.Certificate); err != nil {
		return nil, err
	}
	if _, ok := signer.Public().(ed25519.PublicKey); ok {
		return signer.Sign(rand.Reader, digest, crypto.Hash(0))
	}
	return signer.Sign(rand.Reader, digest, crypto.SHA256)
}

func verifySignature(cert *x509.Certificate, digest, signature []byte) error {
	if cert == nil {
		return fmt.Errorf("certificate is required")
	}
	if len(signature) == 0 {
		return fmt.Errorf("signature is required")
	}
	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		return rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest, signature)
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(pub, digest, signature) {
			return fmt.Errorf("invalid ECDSA signature")
		}
		return nil
	case ed25519.PublicKey:
		if !ed25519.Verify(pub, digest, signature) {
			return fmt.Errorf("invalid Ed25519 signature")
		}
		return nil
	default:
		return fmt.Errorf("unsupported certificate public key type %T", cert.PublicKey)
	}
}

func verifyClientCertificate(rawCerts [][]byte, roots *x509.CertPool) (*x509.Certificate, string, error) {
	if len(rawCerts) == 0 {
		return nil, "", fmt.Errorf("client certificate is required")
	}
	if roots == nil {
		return nil, "", fmt.Errorf("node CA roots are required")
	}
	certs, err := parseCertificates(rawCerts)
	if err != nil {
		return nil, "", err
	}
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	if _, err := certs[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return nil, "", fmt.Errorf("verify client certificate chain: %w", err)
	}
	nodeID, err := identity.PeerNodeID(tls.ConnectionState{PeerCertificates: certs})
	if err != nil {
		return nil, "", err
	}
	return certs[0], nodeID, nil
}

func parseLeafCertificate(rawCerts [][]byte) (*x509.Certificate, error) {
	if len(rawCerts) == 0 {
		return nil, fmt.Errorf("certificate is required")
	}
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return cert, nil
}

func parseCertificates(rawCerts [][]byte) ([]*x509.Certificate, error) {
	certs := make([]*x509.Certificate, 0, len(rawCerts))
	for _, raw := range rawCerts {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	return certs, nil
}

func cloneChain(chain [][]byte) [][]byte {
	cloned := make([][]byte, len(chain))
	for i, cert := range chain {
		cloned[i] = append([]byte(nil), cert...)
	}
	return cloned
}

func writeString(buf *bytes.Buffer, value string) {
	writeBytes(buf, []byte(value))
}

func writeBytes(buf *bytes.Buffer, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	buf.Write(length[:])
	buf.Write(value)
}
