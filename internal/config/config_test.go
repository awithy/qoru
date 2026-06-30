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
		ServiceIdentity: ServiceIdentityConfig{CA: "service-ca.crt"},
		Servers: []ServerConfig{{
			ID:      "server-1",
			Address: "127.0.0.1:4433",
		}},
		Forwards: []ForwardConfig{{
			Protocol: "tcp",
			Listen:   "127.0.0.1:15432",
			Service:  "echo",
			Egress:   "server-1",
		}},
		Routes: []ServiceRouteConfig{{
			Service:   "echo",
			Protocol:  "tcp",
			Selection: RouteSelectionOrdered,
			Candidates: []RouteCandidateConfig{{
				Egress: "server-1",
				Route:  []string{"server-1"},
			}},
		}},
		Services: []ServiceConfig{{
			Name:     "echo",
			Protocol: "tcp",
			Target:   "127.0.0.1:9000",
			E2E:      ServiceE2EConfig{Cert: "echo.crt", Key: "echo.key"},
		}},
	}

	if cfg.NodeID != "client-1" || cfg.Mode != ModeClient {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Address != "127.0.0.1:4433" {
		t.Fatalf("unexpected servers config: %#v", cfg.Servers)
	}
	if len(cfg.Forwards) != 1 || cfg.Forwards[0].Service != "echo" {
		t.Fatalf("unexpected forwards: %#v", cfg.Forwards)
	}
	if cfg.ServiceIdentity.CA != "service-ca.crt" {
		t.Fatalf("unexpected service identity: %#v", cfg.ServiceIdentity)
	}
	if len(cfg.Routes) != 1 || len(cfg.Routes[0].Candidates) != 1 || cfg.Routes[0].Candidates[0].Egress != "server-1" {
		t.Fatalf("unexpected routes: %#v", cfg.Routes)
	}
	if len(cfg.Services) != 1 || cfg.Services[0].E2E.Cert != "echo.crt" || cfg.Services[0].E2E.Key != "echo.key" {
		t.Fatalf("unexpected services: %#v", cfg.Services)
	}
}
