package server

import (
	"testing"

	"github.com/awithy/qoru/internal/config"
)

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
