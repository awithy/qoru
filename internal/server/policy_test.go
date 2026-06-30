package server

import (
	"testing"

	"github.com/awithy/qoru/internal/config"
)

func TestAuthorizeRelayIngressAllowsAnyClientWhenListIsEmpty(t *testing.T) {
	cfg := &config.Config{}
	if err := authorizeRelayIngress(cfg, "client-1"); err != nil {
		t.Fatalf("expected relay ingress to be allowed, got %v", err)
	}
}

func TestAuthorizeRelayIngressAllowsListedClient(t *testing.T) {
	cfg := &config.Config{AllowedRelayClients: []string{"client-1"}}
	if err := authorizeRelayIngress(cfg, "client-1"); err != nil {
		t.Fatalf("expected relay ingress to be allowed, got %v", err)
	}
}

func TestAuthorizeRelayIngressRejectsUnlistedClient(t *testing.T) {
	cfg := &config.Config{AllowedRelayClients: []string{"client-1"}}
	if err := authorizeRelayIngress(cfg, "client-2"); err == nil {
		t.Fatal("expected relay ingress to be rejected")
	}
}

func TestRequireConfiguredPeerAllowsListedPeer(t *testing.T) {
	cfg := &config.Config{Peers: []config.PeerConfig{{ID: "relay-a"}}}
	if err := requireConfiguredPeer(cfg, "relay-a"); err != nil {
		t.Fatalf("expected configured peer to be allowed, got %v", err)
	}
}

func TestRequireConfiguredPeerRejectsUnlistedPeer(t *testing.T) {
	cfg := &config.Config{Peers: []config.PeerConfig{{ID: "relay-a"}}}
	if err := requireConfiguredPeer(cfg, "relay-b"); err == nil {
		t.Fatal("expected unconfigured peer to be rejected")
	}
}

func TestResolveServiceAllowsServiceWithoutPeers(t *testing.T) {
	cfg := &config.Config{Services: []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: "127.0.0.1:9000"}}}
	svc, err := resolveService(cfg, "client-1", "tcp", "echo")
	if err != nil {
		t.Fatalf("expected service to be allowed, got %v", err)
	}
	if svc.Target != "127.0.0.1:9000" {
		t.Fatalf("unexpected target %q", svc.Target)
	}
}

func TestResolveServiceAllowsListedPeer(t *testing.T) {
	cfg := &config.Config{Services: []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: "127.0.0.1:9000", Peers: []string{"client-1"}}}}
	if _, err := resolveService(cfg, "client-1", "tcp", "echo"); err != nil {
		t.Fatalf("expected service to be allowed, got %v", err)
	}
}

func TestResolveServiceRejectsUnlistedPeer(t *testing.T) {
	cfg := &config.Config{Services: []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: "127.0.0.1:9000", Peers: []string{"client-1"}}}}
	if _, err := resolveService(cfg, "client-2", "tcp", "echo"); err == nil {
		t.Fatal("expected peer to be rejected")
	}
}

func TestResolveServiceRejectsMissingService(t *testing.T) {
	cfg := &config.Config{Services: []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: "127.0.0.1:9000"}}}
	if _, err := resolveService(cfg, "client-1", "tcp", "missing"); err == nil {
		t.Fatal("expected missing service to be rejected")
	}
}
