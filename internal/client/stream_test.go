package client

import (
	"context"
	"errors"
	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/protocol"
	"io"
	"log/slog"
	"strings"
	"testing"
)

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
	clientCfg := testClientConfig(addr)

	conn, err := connectTestClient(ctx, clientCfg, logger)
	if err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	streamA, err := OpenTCPStream(ctx, conn, "018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a0001", "echo-a", "", nil, false)
	if err != nil {
		t.Fatalf("open stream A: %v", err)
	}
	streamB, err := OpenTCPStream(ctx, conn, "018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a0002", "echo-b", "", nil, false)
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
	clientCfg := testClientConfig(addr)

	conn, err := connectTestClient(ctx, clientCfg, logger)
	if err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	_, err = OpenTCPStream(ctx, conn, "018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a1234", "echo", "", nil, false)
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

func TestOpenTCPStreamReturnsEgressError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	target := startEchoTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: target.Addr().String(), Peers: []string{"client-1"}}}
	addr, serverErr := startTestServerWithConfig(t, ctx, logger, serverCfg, nil)
	clientCfg := testClientConfig(addr)

	conn, err := connectTestClient(ctx, clientCfg, logger)
	if err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	_, err = OpenTCPStream(ctx, conn, "018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a1234", "echo", "server-2", nil, false)
	if err == nil {
		t.Fatal("expected egress error")
	}
	var rejected *ConnectRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("expected ConnectRejectedError, got %T: %v", err, err)
	}
	if !strings.Contains(rejected.Message, "not reachable") {
		t.Fatalf("unexpected rejection message: %q", rejected.Message)
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
	clientCfg := testClientConfig(addr)

	conn, err := connectTestClient(ctx, clientCfg, logger)
	if err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	_, err = OpenTCPStream(ctx, conn, "018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a1234", "echo", "", nil, false)
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

func TestOpenTCPStreamProxiesBytesToTarget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	received := make(chan protocol.ConnectRequest, 1)
	targetListener := startEchoTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetListener.Addr().String(), Peers: []string{"client-1"}}}
	addr, serverErr := startTestServerWithConfig(t, ctx, logger, serverCfg, func(req protocol.ConnectRequest) { received <- req })

	clientCfg := testClientConfig(addr)
	conn, err := connectTestClient(ctx, clientCfg, logger)
	if err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	stream, err := OpenTCPStream(ctx, conn, "018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a1234", "echo", "", nil, false)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}

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

	req := waitForConnectRequest(t, received)
	if req.RequestID != "018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a1234" {
		t.Fatalf("expected request id to be forwarded, got %q", req.RequestID)
	}
	if req.Protocol != "tcp" {
		t.Fatalf("expected protocol tcp, got %q", req.Protocol)
	}
	if req.Service != "echo" {
		t.Fatalf("expected service echo, got %q", req.Service)
	}

	cancelAndWaitForServer(t, cancel, serverErr)
}
