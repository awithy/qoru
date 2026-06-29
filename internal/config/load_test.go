package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsYAMLConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.yaml")
	input := `node_id: client-1
mode: client
identity:
  cert: client.crt
  key: client.key
  ca: ca.crt
server:
  id: server-1
  address: 127.0.0.1:4433
forwards:
  - protocol: tcp
    listen: 127.0.0.1:15432
    target: 127.0.0.1:5432
`
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.NodeID != "client-1" || cfg.Server == nil || len(cfg.Forwards) != 1 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestMarshalYAML(t *testing.T) {
	out, err := MarshalYAML(Config{NodeID: "server-1", Mode: ModeServer, Listen: "127.0.0.1:4433"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "node_id: server-1") || !strings.Contains(string(out), "mode: server") {
		t.Fatalf("unexpected yaml:\n%s", out)
	}
}
