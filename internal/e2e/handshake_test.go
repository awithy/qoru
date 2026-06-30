package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/awithy/qoru/internal/requestid"
)

func TestHandshakeCoreHappyPath(t *testing.T) {
	ctx := testContext(t)
	nodeCA, nodeKey, nodeRoots := makeTestCA(t, "node-ca")
	serviceCA, serviceKey, serviceRoots := makeTestCA(t, "service-ca")
	clientCert := makeTestLeaf(t, nodeCA, nodeKey, "spiffe://qoru/node/client-1", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	serviceCert := makeTestLeaf(t, serviceCA, serviceKey, "spiffe://qoru/service/echo", []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})

	clientEphemeral, err := GenerateEphemeralKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateEphemeralKey client: %v", err)
	}
	serverEphemeral, err := GenerateEphemeralKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateEphemeralKey server: %v", err)
	}

	clientHello, err := NewClientHello(ctx, clientCert, clientEphemeral.Public)
	if err != nil {
		t.Fatalf("NewClientHello: %v", err)
	}
	clientID, clientLeaf, err := VerifyClientHello(ctx, clientHello, nodeRoots)
	if err != nil {
		t.Fatalf("VerifyClientHello: %v", err)
	}
	if clientID != "client-1" {
		t.Fatalf("unexpected client id %q", clientID)
	}

	serverHello, err := NewServerHello(ctx, serviceCert, clientHello.EphemeralPublicKey, serverEphemeral.Public)
	if err != nil {
		t.Fatalf("NewServerHello: %v", err)
	}
	if _, err := VerifyServerHello(ctx, serverHello, serviceRoots, clientHello.EphemeralPublicKey); err != nil {
		t.Fatalf("VerifyServerHello: %v", err)
	}

	finished, err := NewClientFinished(ctx, clientCert, clientHello.EphemeralPublicKey, serverHello.EphemeralPublicKey)
	if err != nil {
		t.Fatalf("NewClientFinished: %v", err)
	}
	if err := VerifyClientFinished(ctx, finished, clientLeaf, clientHello.EphemeralPublicKey, serverHello.EphemeralPublicKey); err != nil {
		t.Fatalf("VerifyClientFinished: %v", err)
	}

	clientSecret, err := SharedSecret(clientEphemeral.Private, serverHello.EphemeralPublicKey)
	if err != nil {
		t.Fatalf("SharedSecret client: %v", err)
	}
	serverSecret, err := SharedSecret(serverEphemeral.Private, clientHello.EphemeralPublicKey)
	if err != nil {
		t.Fatalf("SharedSecret server: %v", err)
	}
	if !reflect.DeepEqual(clientSecret, serverSecret) {
		t.Fatal("client and server shared secrets differ")
	}

	transcriptHash, err := TranscriptHash(ctx, clientHello.EphemeralPublicKey, serverHello.EphemeralPublicKey)
	if err != nil {
		t.Fatalf("TranscriptHash: %v", err)
	}
	clientKeys, err := DeriveTrafficKeys(clientSecret, transcriptHash)
	if err != nil {
		t.Fatalf("DeriveTrafficKeys client: %v", err)
	}
	serverKeys, err := DeriveTrafficKeys(serverSecret, transcriptHash)
	if err != nil {
		t.Fatalf("DeriveTrafficKeys server: %v", err)
	}
	if !reflect.DeepEqual(clientKeys, serverKeys) {
		t.Fatalf("traffic keys differ: %#v != %#v", clientKeys, serverKeys)
	}
	if len(clientKeys.ClientToServer) != trafficKeySize || len(clientKeys.ServerToClient) != trafficKeySize {
		t.Fatalf("unexpected traffic key sizes: %#v", clientKeys)
	}
}

func TestVerifyServerHelloRejectsWrongServiceIdentity(t *testing.T) {
	ctx := testContext(t)
	serviceCA, serviceKey, serviceRoots := makeTestCA(t, "service-ca")
	wrongServiceCert := makeTestLeaf(t, serviceCA, serviceKey, "spiffe://qoru/service/not-echo", []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	clientEphemeral := mustEphemeral(t)
	serverEphemeral := mustEphemeral(t)

	serverHello, err := NewServerHello(ctx, wrongServiceCert, clientEphemeral.Public, serverEphemeral.Public)
	if err != nil {
		t.Fatalf("NewServerHello: %v", err)
	}
	if _, err := VerifyServerHello(ctx, serverHello, serviceRoots, clientEphemeral.Public); err == nil {
		t.Fatal("expected wrong service identity to be rejected")
	}
}

func TestVerifyServerHelloRejectsWrongCA(t *testing.T) {
	ctx := testContext(t)
	serviceCA, serviceKey, _ := makeTestCA(t, "service-ca")
	_, _, wrongRoots := makeTestCA(t, "wrong-service-ca")
	serviceCert := makeTestLeaf(t, serviceCA, serviceKey, "spiffe://qoru/service/echo", []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	clientEphemeral := mustEphemeral(t)
	serverEphemeral := mustEphemeral(t)

	serverHello, err := NewServerHello(ctx, serviceCert, clientEphemeral.Public, serverEphemeral.Public)
	if err != nil {
		t.Fatalf("NewServerHello: %v", err)
	}
	if _, err := VerifyServerHello(ctx, serverHello, wrongRoots, clientEphemeral.Public); err == nil {
		t.Fatal("expected wrong CA to be rejected")
	}
}

func TestVerifyHelloRejectsTamperedContext(t *testing.T) {
	ctx := testContext(t)
	tampered := ctx
	tampered.Route = []string{"relay-a", "relay-c"}
	nodeCA, nodeKey, nodeRoots := makeTestCA(t, "node-ca")
	clientCert := makeTestLeaf(t, nodeCA, nodeKey, "spiffe://qoru/node/client-1", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	clientEphemeral := mustEphemeral(t)

	clientHello, err := NewClientHello(ctx, clientCert, clientEphemeral.Public)
	if err != nil {
		t.Fatalf("NewClientHello: %v", err)
	}
	if _, _, err := VerifyClientHello(tampered, clientHello, nodeRoots); err == nil {
		t.Fatal("expected tampered context to be rejected")
	}
}

func TestVerifyServerHelloRejectsTamperedEphemeral(t *testing.T) {
	ctx := testContext(t)
	serviceCA, serviceKey, serviceRoots := makeTestCA(t, "service-ca")
	serviceCert := makeTestLeaf(t, serviceCA, serviceKey, "spiffe://qoru/service/echo", []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	clientEphemeral := mustEphemeral(t)
	serverEphemeral := mustEphemeral(t)

	serverHello, err := NewServerHello(ctx, serviceCert, clientEphemeral.Public, serverEphemeral.Public)
	if err != nil {
		t.Fatalf("NewServerHello: %v", err)
	}
	serverHello.EphemeralPublicKey = append([]byte(nil), serverHello.EphemeralPublicKey...)
	serverHello.EphemeralPublicKey[0] ^= 0x01
	if _, err := VerifyServerHello(ctx, serverHello, serviceRoots, clientEphemeral.Public); err == nil {
		t.Fatal("expected tampered server ephemeral key to be rejected")
	}
}

func TestVerifyClientFinishedRejectsBadSignature(t *testing.T) {
	ctx := testContext(t)
	nodeCA, nodeKey, nodeRoots := makeTestCA(t, "node-ca")
	clientCert := makeTestLeaf(t, nodeCA, nodeKey, "spiffe://qoru/node/client-1", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	clientEphemeral := mustEphemeral(t)
	serverEphemeral := mustEphemeral(t)
	clientHello, err := NewClientHello(ctx, clientCert, clientEphemeral.Public)
	if err != nil {
		t.Fatalf("NewClientHello: %v", err)
	}
	_, clientLeaf, err := VerifyClientHello(ctx, clientHello, nodeRoots)
	if err != nil {
		t.Fatalf("VerifyClientHello: %v", err)
	}
	finished, err := NewClientFinished(ctx, clientCert, clientEphemeral.Public, serverEphemeral.Public)
	if err != nil {
		t.Fatalf("NewClientFinished: %v", err)
	}
	finished.Signature = append([]byte(nil), finished.Signature...)
	finished.Signature[len(finished.Signature)-1] ^= 0x01
	if err := VerifyClientFinished(ctx, finished, clientLeaf, clientEphemeral.Public, serverEphemeral.Public); err == nil {
		t.Fatal("expected bad client finished signature to be rejected")
	}
}

func testContext(t *testing.T) Context {
	t.Helper()
	reqID, err := requestid.New()
	if err != nil {
		t.Fatalf("requestid.New: %v", err)
	}
	return Context{RequestID: reqID, Service: "echo", Egress: "relay-b", Route: []string{"relay-a", "relay-b"}}
}

func mustEphemeral(t *testing.T) EphemeralKeyPair {
	t.Helper()
	key, err := GenerateEphemeralKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateEphemeralKey: %v", err)
	}
	return key
}

func makeTestCA(t *testing.T, commonName string) (*x509.Certificate, *rsa.PrivateKey, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey CA: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("rand.Int CA serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("x509.CreateCertificate CA: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("x509.ParseCertificate CA: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return cert, key, pool
}

func makeTestLeaf(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, spiffeURI string, usages []x509.ExtKeyUsage) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey leaf: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("rand.Int leaf serial: %v", err)
	}
	uri, err := url.Parse(spiffeURI)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: spiffeURI},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           usages,
		BasicConstraintsValid: true,
		URIs:                  []*url.URL{uri},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("x509.CreateCertificate leaf: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("x509.ParseCertificate leaf: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}
}
