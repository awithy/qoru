package client

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/awithy/qoru/internal/server"
	"github.com/quic-go/quic-go"
)

func TestRunListensAndProxiesLocalTCP(t *testing.T) {
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetListener := startEchoTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetListener.Addr().String(), Peers: []string{"client-1"}}}
	addr, serverErr := startTestServerWithConfig(t, serverCtx, logger, serverCfg, nil)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	clientCfg := testClientConfig(addr, targetListener.Addr().String())
	clientCfg.Forwards[0].Listen = "127.0.0.1:0"

	clientStarted := make(chan string, 1)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { clientStarted <- addr }))
	}()

	var clientAddr string
	select {
	case clientAddr = <-clientStarted:
	case err := <-clientErr:
		t.Fatalf("client exited before starting: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client to start")
	}

	localConn, err := net.Dial("tcp", clientAddr)
	if err != nil {
		t.Fatalf("dial client listener: %v", err)
	}
	defer localConn.Close()

	if _, err := localConn.Write([]byte("ping")); err != nil {
		t.Fatalf("write to local connection: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(localConn, buf); err != nil {
		t.Fatalf("read from local connection: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("expected echo %q, got %q", "ping", string(buf))
	}

	clientCancel()
	select {
	case err := <-clientErr:
		if err != nil {
			t.Fatalf("expected clean client shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client shutdown")
	}
	cancelAndWaitForServer(t, serverCancel, serverErr)
}

func TestRunListensOnMultipleForwards(t *testing.T) {
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetA := startEchoTCPServer(t)
	targetB := startEchoTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.Services = []config.ServiceConfig{
		{Name: "echo-a", Protocol: "tcp", Target: targetA.Addr().String(), Peers: []string{"client-1"}},
		{Name: "echo-b", Protocol: "tcp", Target: targetB.Addr().String(), Peers: []string{"client-1"}},
	}
	addr, serverErr := startTestServerWithConfig(t, serverCtx, logger, serverCfg, nil)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	clientCfg := testClientConfig(addr, targetA.Addr().String())
	clientCfg.Forwards = []config.ForwardConfig{
		{Protocol: "tcp", Listen: "127.0.0.1:0", Service: "echo-a"},
		{Protocol: "tcp", Listen: "127.0.0.1:0", Service: "echo-b"},
	}

	started := make(chan string, 2)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { started <- addr }))
	}()

	clientAddrA := waitForClientAddr(t, started, clientErr)
	clientAddrB := waitForClientAddr(t, started, clientErr)

	assertEcho(t, clientAddrA, "one!")
	assertEcho(t, clientAddrB, "two!")

	clientCancel()
	select {
	case err := <-clientErr:
		if err != nil {
			t.Fatalf("expected clean client shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client shutdown")
	}
	cancelAndWaitForServer(t, serverCancel, serverErr)
}

func TestRunReconnectsForNewLocalTCPConnections(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetListener := startEchoTCPServerLoop(t)

	serverCfg := testServerConfig()
	serverCfg.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetListener.Addr().String(), Peers: []string{"client-1"}}}

	serverCtx, serverCancel := context.WithCancel(context.Background())
	addr, serverErr := startTestServerWithConfig(t, serverCtx, logger, serverCfg, nil)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	clientCfg := testClientConfig(addr, targetListener.Addr().String())
	clientCfg.Forwards[0].Listen = "127.0.0.1:0"

	clientStarted := make(chan string, 1)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { clientStarted <- addr }))
	}()

	clientAddr := waitForClientAddr(t, clientStarted, clientErr)
	assertEcho(t, clientAddr, "one!")

	cancelAndWaitForServer(t, serverCancel, serverErr)

	serverCtx2, serverCancel2 := context.WithCancel(context.Background())
	defer serverCancel2()
	addr2, serverErr2 := startTestServerWithConfig(t, serverCtx2, logger, serverCfg, nil)
	clientCfg.Server.Address = addr2

	assertEcho(t, clientAddr, "two!")

	clientCancel()
	select {
	case err := <-clientErr:
		if err != nil {
			t.Fatalf("expected clean client shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client shutdown")
	}
	cancelAndWaitForServer(t, serverCancel2, serverErr2)
}

func TestMultipleStreamsOnOneQUICConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetA := startEchoTCPServer(t)
	targetB := startEchoTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.Services = []config.ServiceConfig{
		{Name: "echo-a", Protocol: "tcp", Target: targetA.Addr().String(), Peers: []string{"client-1"}},
		{Name: "echo-b", Protocol: "tcp", Target: targetB.Addr().String(), Peers: []string{"client-1"}},
	}
	addr, serverErr := startTestServerWithConfig(t, ctx, logger, serverCfg, nil)
	clientCfg := testClientConfig(addr, targetA.Addr().String())

	conn, err := Connect(ctx, clientCfg, logger)
	if err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	streamA, err := OpenTCPStream(ctx, conn, "echo-a", "")
	if err != nil {
		t.Fatalf("open stream A: %v", err)
	}
	streamB, err := OpenTCPStream(ctx, conn, "echo-b", "")
	if err != nil {
		t.Fatalf("open stream B: %v", err)
	}

	assertStreamEcho(t, streamA, "aaaa")
	assertStreamEcho(t, streamB, "bbbb")

	cancelAndWaitForServer(t, cancel, serverErr)
}

func TestOpenTCPStreamReturnsTargetDialError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	serverCfg := testServerConfig()
	serverCfg.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: "127.0.0.1:1", Peers: []string{"client-1"}}}
	addr, serverErr := startTestServerWithConfig(t, ctx, logger, serverCfg, nil)
	clientCfg := testClientConfig(addr, "127.0.0.1:1")

	conn, err := Connect(ctx, clientCfg, logger)
	if err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	_, err = OpenTCPStream(ctx, conn, "echo", "")
	if err == nil {
		t.Fatal("expected target dial error")
	}
	var rejected *ConnectRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("expected ConnectRejectedError, got %T: %v", err, err)
	}
	if rejected.Message == "" {
		t.Fatal("expected rejection message")
	}

	cancelAndWaitForServer(t, cancel, serverErr)
}

func TestOpenTCPStreamReturnsTargetPolicyError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	serverCfg := testServerConfig()
	serverCfg.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: "127.0.0.1:9000", Peers: []string{"client-2"}}}
	addr, serverErr := startTestServerWithConfig(t, ctx, logger, serverCfg, nil)
	clientCfg := testClientConfig(addr, "127.0.0.1:9001")

	conn, err := Connect(ctx, clientCfg, logger)
	if err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	_, err = OpenTCPStream(ctx, conn, "echo", "")
	if err == nil {
		t.Fatal("expected target policy error")
	}
	var rejected *ConnectRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("expected ConnectRejectedError, got %T: %v", err, err)
	}
	if !strings.Contains(rejected.Message, "not allowed") {
		t.Fatalf("unexpected rejection message: %q", rejected.Message)
	}

	cancelAndWaitForServer(t, cancel, serverErr)
}

func TestConnectTCPProxiesBytesToTarget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	received := make(chan protocol.ConnectRequest, 1)
	targetListener := startEchoTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetListener.Addr().String(), Peers: []string{"client-1"}}}
	addr, serverErr := startTestServerWithConfig(t, ctx, logger, serverCfg, func(req protocol.ConnectRequest) { received <- req })

	clientCfg := testClientConfig(addr, targetListener.Addr().String())
	conn, stream, err := ConnectTCP(ctx, clientCfg, "echo", "", logger)
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
		if req.Protocol != "tcp" {
			t.Fatalf("expected protocol tcp, got %q", req.Protocol)
		}
		if req.Service != "echo" {
			t.Fatalf("expected service echo, got %q", req.Service)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to receive connect request")
	}

	cancelAndWaitForServer(t, cancel, serverErr)
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
		Identity: config.IdentityConfig{Cert: "../../dev/certs/server-1.crt", Key: "../../dev/certs/server-1.key", CA: "../../dev/certs/ca.crt"},
		Listen:   "127.0.0.1:0",
	}
}

func startTestServerWithConfig(t *testing.T, ctx context.Context, logger *slog.Logger, serverCfg *config.Config, onConnect func(protocol.ConnectRequest)) (string, <-chan error) {
	t.Helper()
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
		return addr, serverErr
	case err := <-serverErr:
		t.Fatalf("server exited before starting: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to start")
	}
	panic("unreachable")
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

func testClientConfig(serverAddr, targetAddr string) *config.Config {
	return &config.Config{
		NodeID:   "client-1",
		Mode:     config.ModeClient,
		Identity: config.IdentityConfig{Cert: "../../dev/certs/client-1.crt", Key: "../../dev/certs/client-1.key", CA: "../../dev/certs/ca.crt"},
		Server:   &config.ServerConfig{ID: "server-1", Address: serverAddr},
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
