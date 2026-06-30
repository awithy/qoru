package client

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/awithy/qoru/internal/server"
	"github.com/awithy/qoru/internal/testcert"
	"github.com/quic-go/quic-go"
)

func waitForConnectRequest(t *testing.T, ch <-chan protocol.ConnectRequest) protocol.ConnectRequest {
	t.Helper()
	select {
	case req := <-ch:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connect request")
		return protocol.ConnectRequest{}
	}
}

func connectTestClient(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*quic.Conn, error) {
	if err := config.ValidateClient(cfg); err != nil {
		return nil, err
	}
	if len(cfg.Servers) != 1 {
		return nil, fmt.Errorf("test client config must have exactly one server")
	}
	return ConnectToServer(ctx, cfg.NodeID, cfg.Identity, cfg.Servers[0], logger)
}

func waitForClientAddr(t *testing.T, started <-chan string, clientErr <-chan error) string {
	t.Helper()
	select {
	case addr := <-started:
		return addr
	case err := <-clientErr:
		t.Fatalf("client exited before starting: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client to start")
	}
	panic("unreachable")
}

func assertStreamEcho(t *testing.T, stream *quic.Stream, msg string) {
	t.Helper()
	if _, err := stream.Write([]byte(msg)); err != nil {
		t.Fatalf("write to stream: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatalf("read from stream: %v", err)
	}
	if string(buf) != msg {
		t.Fatalf("expected echo %q, got %q", msg, string(buf))
	}
}

func assertRead(t *testing.T, addr, want string) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial client listener: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, len(want))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read from local connection: %v", err)
	}
	if string(buf) != want {
		t.Fatalf("expected %q, got %q", want, string(buf))
	}
}

func assertEcho(t *testing.T, addr, msg string) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial client listener: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("write to local connection: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read from local connection: %v", err)
	}
	if string(buf) != msg {
		t.Fatalf("expected echo %q, got %q", msg, string(buf))
	}
}

func testServerConfig() *config.Config {
	return &config.Config{
		NodeID:   "server-1",
		Mode:     config.ModeServer,
		Identity: testcert.MustNodeIdentity("server-1"),
		Listen:   "127.0.0.1:0",
	}
}

func startTestServerWithConfig(t *testing.T, ctx context.Context, logger *slog.Logger, serverCfg *config.Config, onConnect func(protocol.ConnectRequest)) (string, <-chan error) {
	t.Helper()
	return startTestServerWithConfigAttempt(t, ctx, logger, serverCfg, onConnect)
}

func startTestServerWithConfigEventually(t *testing.T, ctx context.Context, logger *slog.Logger, serverCfg *config.Config, onConnect func(protocol.ConnectRequest)) (string, <-chan error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		addr, serverErr, err := tryStartTestServerWithConfig(ctx, logger, serverCfg, onConnect)
		if err == nil {
			return addr, serverErr
		}
		if !strings.Contains(err.Error(), "address already in use") || time.Now().After(deadline) {
			t.Fatalf("server exited before starting: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	panic("unreachable")
}

func startTestServerWithConfigAttempt(t *testing.T, ctx context.Context, logger *slog.Logger, serverCfg *config.Config, onConnect func(protocol.ConnectRequest)) (string, <-chan error) {
	t.Helper()
	addr, serverErr, err := tryStartTestServerWithConfig(ctx, logger, serverCfg, onConnect)
	if err != nil {
		t.Fatalf("server exited before starting: %v", err)
	}
	return addr, serverErr
}

func tryStartTestServerWithConfig(ctx context.Context, logger *slog.Logger, serverCfg *config.Config, onConnect func(protocol.ConnectRequest)) (string, <-chan error, error) {
	started := make(chan string, 1)
	serverErr := make(chan error, 1)
	options := []server.Option{server.WithStartedFunc(func(addr string) { started <- addr })}
	if onConnect != nil {
		options = append(options, server.WithConnectRequestFunc(onConnect))
	}

	go func() {
		serverErr <- server.Run(ctx, serverCfg, logger, options...)
	}()

	select {
	case addr := <-started:
		return addr, serverErr, nil
	case err := <-serverErr:
		return "", nil, err
	case <-time.After(2 * time.Second):
		return "", nil, fmt.Errorf("timed out waiting for server to start")
	}
}

func makeDevNodeCert(t *testing.T, nodeID string) config.IdentityConfig {
	t.Helper()
	return testcert.NodeIdentity(t, nodeID)
}

func closedTCPAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func startFixedTCPServer(t *testing.T, response string) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = conn.Write([]byte(response))
			}()
		}
	}()
	return listener
}

func startReadAllTCPServer(t *testing.T) net.Listener {
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
		body, err := io.ReadAll(conn)
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("ack:" + string(body)))
	}()
	return listener
}

func startDiscardTCPServer(t *testing.T) net.Listener {
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
		_, _ = io.Copy(io.Discard, conn)
	}()
	return listener
}

func startEchoTCPServerLoop(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return listener
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

func testClientConfig(serverAddr string) *config.Config {
	return &config.Config{
		NodeID:   "client-1",
		Mode:     config.ModeClient,
		Identity: testcert.MustNodeIdentity("client-1"),
		Servers:  []config.ServerConfig{{ID: "server-1", Address: serverAddr}},
		Forwards: []config.ForwardConfig{{
			Protocol: "tcp",
			Listen:   "127.0.0.1:15432",
			Service:  "echo",
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
