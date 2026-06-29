package cli

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/awithy/qoru/internal/config"
	"github.com/spf13/cobra"
)

func TestRootCommandHasConfigFlag(t *testing.T) {
	cmd := NewRootCommand()

	flag := cmd.PersistentFlags().Lookup("config")
	if flag == nil {
		t.Fatal("expected --config persistent flag")
	}
	if flag.Shorthand != "c" {
		t.Fatalf("expected -c shorthand, got %q", flag.Shorthand)
	}
}

func TestRootHelpDoesNotRequireConfig(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected help to execute without config, got %v", err)
	}
}

func TestRootHasClientServerAndPrintConfigCommands(t *testing.T) {
	cmd := NewRootCommand()
	for _, name := range []string{"client", "server", "print-config"} {
		if child, _, err := cmd.Find([]string{name}); err != nil || child.Name() != name {
			t.Fatalf("expected %q command, child=%v err=%v", name, child, err)
		}
	}
}

func TestClientCommandLoadsAndValidatesConfig(t *testing.T) {
	path := writeTestConfig(t, `node_id: client-1
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
`)
	cmd := newRootCommand(commandRunners{
		client: func(_ context.Context, cfg *config.Config, logger *slog.Logger) error {
			if err := config.ValidateClient(cfg); err != nil {
				return err
			}
			logger.Info("client runner called", "node_id", cfg.NodeID)
			return nil
		},
		server: runServer,
	})

	out, err := executeCommand(cmd, "client", "-c", path)
	if err != nil {
		t.Fatalf("expected client command to succeed: %v", err)
	}
	if !strings.Contains(out, "msg=\"client runner called\"") || !strings.Contains(out, "node_id=client-1") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestServerCommandLoadsConfigAndCallsRunner(t *testing.T) {
	path := writeTestConfig(t, `node_id: server-1
mode: server
identity:
  cert: server.crt
  key: server.key
  ca: ca.crt
listen: 127.0.0.1:4433
`)

	cmd := newRootCommand(commandRunners{
		client: func(_ context.Context, cfg *config.Config, logger *slog.Logger) error {
			return config.ValidateClient(cfg)
		},
		server: func(_ context.Context, cfg *config.Config, logger *slog.Logger) error {
			if err := config.ValidateServer(cfg); err != nil {
				return err
			}
			logger.Info("server runner called", "node_id", cfg.NodeID)
			return nil
		},
	})

	out, err := executeCommand(cmd, "server", "-c", path)
	if err != nil {
		t.Fatalf("expected server command to succeed: %v", err)
	}
	if !strings.Contains(out, "msg=\"server runner called\"") || !strings.Contains(out, "node_id=server-1") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPrintConfigPrintsValidatedYAML(t *testing.T) {
	path := writeTestConfig(t, `node_id: server-1
mode: server
identity:
  cert: server.crt
  key: server.key
  ca: ca.crt
listen: 127.0.0.1:4433
`)
	out, err := executeCommand(NewRootCommand(), "print-config", "-c", path)
	if err != nil {
		t.Fatalf("expected print-config to succeed: %v", err)
	}
	if !strings.Contains(out, "node_id: server-1") || !strings.Contains(out, "mode: server") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestClientCommandRejectsServerConfig(t *testing.T) {
	path := writeTestConfig(t, `node_id: server-1
mode: server
identity:
  cert: server.crt
  key: server.key
  ca: ca.crt
listen: 127.0.0.1:4433
`)
	_, err := executeCommand(NewRootCommand(), "client", "-c", path)
	if err == nil {
		t.Fatal("expected client command to reject server config")
	}
}

func executeCommand(cmd *cobra.Command, args ...string) (string, error) {
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
