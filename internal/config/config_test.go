package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathUsesExplicitPath(t *testing.T) {
	path, ok := ResolvePath("custom.yaml")
	if !ok {
		t.Fatal("expected explicit path to resolve")
	}
	if path != "custom.yaml" {
		t.Fatalf("expected custom.yaml, got %q", path)
	}
}

func TestResolvePathFindsFirstDefaultPath(t *testing.T) {
	tmp := t.TempDir()
	first := filepath.Join(tmp, "missing.yaml")
	second := filepath.Join(tmp, "qoru.yaml")
	if err := os.WriteFile(second, []byte("node_id: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, ok := ResolvePathWithDefaults("", []string{first, second})
	if !ok {
		t.Fatal("expected default path to resolve")
	}
	if path != second {
		t.Fatalf("expected %q, got %q", second, path)
	}
}

func TestResolvePathReturnsFalseWhenNoDefaultExists(t *testing.T) {
	path, ok := ResolvePathWithDefaults("", []string{filepath.Join(t.TempDir(), "qoru.yaml")})
	if ok {
		t.Fatalf("expected no resolved path, got %q", path)
	}
}

func TestConfigShape(t *testing.T) {
	cfg := Config{
		NodeID: "client-1",
		Mode:   ModeClient,
		Identity: IdentityConfig{
			Cert: "client.crt",
			Key:  "client.key",
			CA:   "ca.crt",
		},
		Server: &ServerConfig{
			ID:      "server-1",
			Address: "127.0.0.1:4433",
		},
		Forwards: []ForwardConfig{{
			Protocol: "tcp",
			Listen:   "127.0.0.1:15432",
			Target:   "127.0.0.1:5432",
		}},
	}

	if cfg.NodeID != "client-1" || cfg.Mode != ModeClient {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if cfg.Server == nil || cfg.Server.Address != "127.0.0.1:4433" {
		t.Fatalf("unexpected server config: %#v", cfg.Server)
	}
	if len(cfg.Forwards) != 1 || cfg.Forwards[0].Target != "127.0.0.1:5432" {
		t.Fatalf("unexpected forwards: %#v", cfg.Forwards)
	}
}
