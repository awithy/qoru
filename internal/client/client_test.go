package client

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/server"
)

func TestRunConnectsToServerWithMTLS(t *testing.T) {
	serverCfg := &config.Config{
		NodeID:   "server-1",
		Mode:     config.ModeServer,
		Identity: config.IdentityConfig{Cert: "../../dev/certs/server-1.crt", Key: "../../dev/certs/server-1.key", CA: "../../dev/certs/ca.crt"},
		Listen:   "127.0.0.1:0",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	started := make(chan string, 1)
	serverErr := make(chan error, 1)

	go func() {
		serverErr <- server.Run(ctx, serverCfg, logger, server.WithStartedFunc(func(addr string) { started <- addr }))
	}()

	var addr string
	select {
	case addr = <-started:
	case err := <-serverErr:
		t.Fatalf("server exited before starting: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to start")
	}

	clientCfg := &config.Config{
		NodeID:   "client-1",
		Mode:     config.ModeClient,
		Identity: config.IdentityConfig{Cert: "../../dev/certs/client-1.crt", Key: "../../dev/certs/client-1.key", CA: "../../dev/certs/ca.crt"},
		Server:   &config.ServerConfig{ID: "server-1", Address: addr},
		TCPForwards: []config.TCPForwardConfig{{
			Listen: "127.0.0.1:15432",
			Target: "127.0.0.1:5432",
		}},
	}

	if err := Run(ctx, clientCfg, logger); err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}

	cancel()
	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("expected clean server shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server shutdown")
	}
}
