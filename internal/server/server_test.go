package server

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/testcert"
)

func TestRunStartsWithUnavailableDialPeer(t *testing.T) {
	cfg := &config.Config{
		NodeID:   "relay-a",
		Mode:     config.ModeServer,
		Identity: testcert.NodeIdentity(t, "relay-a"),
		Listen:   "127.0.0.1:0",
		Peers:    []config.PeerConfig{{ID: "relay-b", Address: "127.0.0.1:1", Dial: true}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan string, 1)
	errCh := make(chan error, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	go func() {
		errCh <- Run(ctx, cfg, logger, WithStartedFunc(func(addr string) { started <- addr }))
	}()

	select {
	case <-started:
	case err := <-errCh:
		t.Fatalf("server exited before starting: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to start")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected clean shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to stop")
	}
}

func TestRunStartsAndStopsQUICServer(t *testing.T) {
	cfg := &config.Config{
		NodeID:   "server-1",
		Mode:     config.ModeServer,
		Identity: testcert.NodeIdentity(t, "server-1"),
		Listen:   "127.0.0.1:0",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan string, 1)
	errCh := make(chan error, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	go func() {
		errCh <- Run(ctx, cfg, logger, WithStartedFunc(func(addr string) { started <- addr }))
	}()

	select {
	case addr := <-started:
		if !strings.HasPrefix(addr, "127.0.0.1:") {
			t.Fatalf("expected localhost listen addr, got %q", addr)
		}
	case err := <-errCh:
		t.Fatalf("server exited before starting: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to start")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected clean shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to stop")
	}
}
