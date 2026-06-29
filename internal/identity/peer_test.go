package identity

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/url"
	"testing"
)

func TestPeerNodeIDUsesSPIFFEURISAN(t *testing.T) {
	uri, err := url.Parse("spiffe://qoru/node/client-1")
	if err != nil {
		t.Fatal(err)
	}
	state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{{
		URIs:     []*url.URL{uri},
		DNSNames: []string{"dns-fallback"},
	}}}

	id, err := PeerNodeID(state)
	if err != nil {
		t.Fatalf("expected peer node id, got %v", err)
	}
	if id != "client-1" {
		t.Fatalf("expected client-1, got %q", id)
	}
}

func TestPeerNodeIDUsesDNSName(t *testing.T) {
	state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{{DNSNames: []string{"client-1"}}}}

	id, err := PeerNodeID(state)
	if err != nil {
		t.Fatalf("expected peer node id, got %v", err)
	}
	if id != "client-1" {
		t.Fatalf("expected client-1, got %q", id)
	}
}

func TestPeerNodeIDUsesCommonNameFallback(t *testing.T) {
	state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: "client-1"}}}}

	id, err := PeerNodeID(state)
	if err != nil {
		t.Fatalf("expected peer node id, got %v", err)
	}
	if id != "client-1" {
		t.Fatalf("expected client-1, got %q", id)
	}
}

func TestPeerNodeIDRejectsMissingCertificate(t *testing.T) {
	_, err := PeerNodeID(tls.ConnectionState{})
	if err == nil {
		t.Fatal("expected missing peer certificate to be rejected")
	}
}
