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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	addr, serverErr := startTestServer(t, ctx, logger, nil)
	targetListener, targetAccepted := startAcceptOnlyTCPServer(t)

	clientCfg := testClientConfig(addr, targetListener.Addr().String())
	if err := Run(ctx, clientCfg, logger); err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}

	select {
	case <-targetAccepted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to dial target")
	}

	cancelAndWaitForServer(t, cancel, serverErr)
}

func TestConnectTCPProxiesBytesToTarget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	received := make(chan protocol.ConnectTCPRequest, 1)
	addr, serverErr := startTestServer(t, ctx, logger, func(req protocol.ConnectTCPRequest) { received <- req })
	targetListener := startEchoTCPServer(t)

	clientCfg := testClientConfig(addr, targetListener.Addr().String())
	conn, stream, err := ConnectTCP(ctx, clientCfg, targetListener.Addr().String(), logger)
	if err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	if _, err := stream.Write([]byte("ping")); err != nil {
		t.Fatalf("write to stream: %v", err)
	}

	buf := make([]byte, 4)
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatalf("read from stream: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("expected echo %q, got %q", "ping", string(buf))
	}

	select {
	case req := <-received:
		if req.Target != targetListener.Addr().String() {
			t.Fatalf("expected target %q, got %q", targetListener.Addr().String(), req.Target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to receive connect tcp request")
	}

	cancelAndWaitForServer(t, cancel, serverErr)
}

func startTestServer(t *testing.T, ctx context.Context, logger *slog.Logger, onConnect func(protocol.ConnectTCPRequest)) (string, <-chan error) {
	t.Helper()
	serverCfg := &config.Config{
		NodeID:   "server-1",
		Mode:     config.ModeServer,
		Identity: config.IdentityConfig{Cert: "../../dev/certs/server-1.crt", Key: "../../dev/certs/server-1.key", CA: "../../dev/certs/ca.crt"},
		Listen:   "127.0.0.1:0",
	}

	started := make(chan string, 1)
	serverErr := make(chan error, 1)
	options := []server.Option{server.WithStartedFunc(func(addr string) { started <- addr })}
	if onConnect != nil {
		options = append(options, server.WithConnectTCPRequestFunc(onConnect))
	}

	go func() {
		serverErr <- server.Run(ctx, serverCfg, logger, options...)
	}()

	select {
	case addr := <-started:
		return addr, serverErr
	case err := <-serverErr:
		t.Fatalf("server exited before starting: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to start")
	}
	panic("unreachable")
}

func startAcceptOnlyTCPServer(t *testing.T) (net.Listener, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
		accepted <- struct{}{}
	}()
	return listener, accepted
}

func startEchoTCPServer(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()
	return listener
}

func testClientConfig(serverAddr, targetAddr string) *config.Config {
	return &config.Config{
		NodeID:   "client-1",
		Mode:     config.ModeClient,
		Identity: config.IdentityConfig{Cert: "../../dev/certs/client-1.crt", Key: "../../dev/certs/client-1.key", CA: "../../dev/certs/ca.crt"},
		Server:   &config.ServerConfig{ID: "server-1", Address: serverAddr},
		TCPForwards: []config.TCPForwardConfig{{
			Listen: "127.0.0.1:15432",
			Target: targetAddr,
		}},
	}
}

func cancelAndWaitForServer(t *testing.T, cancel context.CancelFunc, serverErr <-chan error) {
	t.Helper()
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
