package server

import (
	"testing"

	"github.com/awithy/qoru/internal/config"
)

func TestAuthorizeTCPTargetAllowsAnyTargetWhenAllowlistEmpty(t *testing.T) {
	cfg := &config.Config{}
	if err := authorizeTCPTarget(cfg, "client-1", "127.0.0.1:9000"); err != nil {
		t.Fatalf("expected target to be allowed, got %v", err)
	}
}

func TestAuthorizeTCPTargetAllowsListedTargetWithoutPeers(t *testing.T) {
	cfg := &config.Config{AllowedTargets: []config.AllowedTargetConfig{{Protocol: "tcp", Address: "127.0.0.1:9000"}}}
	if err := authorizeTCPTarget(cfg, "client-1", "127.0.0.1:9000"); err != nil {
		t.Fatalf("expected target to be allowed, got %v", err)
	}
}

func TestAuthorizeTCPTargetAllowsListedPeer(t *testing.T) {
	cfg := &config.Config{AllowedTargets: []config.AllowedTargetConfig{{Protocol: "tcp", Address: "127.0.0.1:9000", Peers: []string{"client-1"}}}}
	if err := authorizeTCPTarget(cfg, "client-1", "127.0.0.1:9000"); err != nil {
		t.Fatalf("expected target to be allowed, got %v", err)
	}
}

func TestAuthorizeTCPTargetRejectsUnlistedPeer(t *testing.T) {
	cfg := &config.Config{AllowedTargets: []config.AllowedTargetConfig{{Protocol: "tcp", Address: "127.0.0.1:9000", Peers: []string{"client-1"}}}}
	if err := authorizeTCPTarget(cfg, "client-2", "127.0.0.1:9000"); err == nil {
		t.Fatal("expected peer to be rejected")
	}
}

func TestAuthorizeTCPTargetIgnoresListedUDPTarget(t *testing.T) {
	cfg := &config.Config{AllowedTargets: []config.AllowedTargetConfig{{Protocol: "udp", Address: "127.0.0.1:9000"}}}
	if err := authorizeTCPTarget(cfg, "client-1", "127.0.0.1:9000"); err == nil {
		t.Fatal("expected tcp target to be rejected when only udp target is listed")
	}
}

func TestAuthorizeTCPTargetRejectsUnlistedTarget(t *testing.T) {
	cfg := &config.Config{AllowedTargets: []config.AllowedTargetConfig{{Protocol: "tcp", Address: "127.0.0.1:9000"}}}
	if err := authorizeTCPTarget(cfg, "client-1", "127.0.0.1:9001"); err == nil {
		t.Fatal("expected target to be rejected")
	}
}
