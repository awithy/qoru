package client

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/protocol"
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
	received := make(chan protocol.ConnectTCPRequest, 1)
	serverErr := make(chan error, 1)

	go func() {
		serverErr <- server.Run(ctx, serverCfg, logger,
			server.WithStartedFunc(func(addr string) { started <- addr }),
			server.WithConnectTCPRequestFunc(func(req protocol.ConnectTCPRequest) { received <- req }),
		)
	}()

	var addr string
	select {
	case addr = <-started:
	case err := <-serverErr:
		t.Fatalf("server exited before starting: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to start")
	}

	targetListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer targetListener.Close()

	targetAccepted := make(chan struct{}, 1)
	go func() {
		conn, err := targetListener.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
		targetAccepted <- struct{}{}
	}()

	clientCfg := &config.Config{
		NodeID:   "client-1",
		Mode:     config.ModeClient,
		Identity: config.IdentityConfig{Cert: "../../dev/certs/client-1.crt", Key: "../../dev/certs/client-1.key", CA: "../../dev/certs/ca.crt"},
		Server:   &config.ServerConfig{ID: "server-1", Address: addr},
		TCPForwards: []config.TCPForwardConfig{{
			Listen: "127.0.0.1:15432",
			Target: targetListener.Addr().String(),
		}},
	}

	if err := Run(ctx, clientCfg, logger); err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}

	select {
	case req := <-received:
		if req.Target != targetListener.Addr().String() {
			t.Fatalf("expected target %q, got %q", targetListener.Addr().String(), req.Target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to receive connect tcp request")
	}

	select {
	case <-targetAccepted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to dial target")
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
