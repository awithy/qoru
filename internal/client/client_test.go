package client

import (
	"context"
	"github.com/awithy/qoru/internal/config"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
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
	clientCfg := testClientConfig(addr)
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

func TestRunAllowsClientHalfCloseBeforeTargetResponse(t *testing.T) {
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetListener := startReadAllTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetListener.Addr().String(), Peers: []string{"client-1"}}}
	addr, serverErr := startTestServerWithConfig(t, serverCtx, logger, serverCfg, nil)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	clientCfg := testClientConfig(addr)
	clientCfg.Forwards[0].Listen = "127.0.0.1:0"

	clientStarted := make(chan string, 1)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { clientStarted <- addr }))
	}()

	clientAddr := waitForClientAddr(t, clientStarted, clientErr)
	tcpAddr, err := net.ResolveTCPAddr("tcp", clientAddr)
	if err != nil {
		t.Fatalf("resolve client listener: %v", err)
	}
	localConn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		t.Fatalf("dial client listener: %v", err)
	}
	defer localConn.Close()

	if _, err := localConn.Write([]byte("half-close")); err != nil {
		t.Fatalf("write to local connection: %v", err)
	}
	if err := localConn.CloseWrite(); err != nil {
		t.Fatalf("half-close local connection: %v", err)
	}

	want := "ack:half-close"
	buf := make([]byte, len(want))
	if _, err := io.ReadFull(localConn, buf); err != nil {
		t.Fatalf("read response after half-close: %v", err)
	}
	if string(buf) != want {
		t.Fatalf("expected %q, got %q", want, string(buf))
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

func TestRunShutdownClosesActiveLocalConnection(t *testing.T) {
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetListener := startDiscardTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetListener.Addr().String(), Peers: []string{"client-1"}}}
	addr, serverErr := startTestServerWithConfig(t, serverCtx, logger, serverCfg, nil)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	clientCfg := testClientConfig(addr)
	clientCfg.Forwards[0].Listen = "127.0.0.1:0"

	clientStarted := make(chan string, 1)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { clientStarted <- addr }))
	}()

	clientAddr := waitForClientAddr(t, clientStarted, clientErr)
	localConn, err := net.Dial("tcp", clientAddr)
	if err != nil {
		t.Fatalf("dial client listener: %v", err)
	}
	defer localConn.Close()
	if _, err := localConn.Write([]byte("still-open")); err != nil {
		t.Fatalf("write to local connection: %v", err)
	}

	clientCancel()
	select {
	case err := <-clientErr:
		if err != nil {
			t.Fatalf("expected clean client shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client shutdown with active connection")
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
	clientCfg := testClientConfig(addr)
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
	clientCfg := testClientConfig(addr)
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
	serverCfg2 := *serverCfg
	serverCfg2.Listen = addr
	addr2, serverErr2 := startTestServerWithConfigEventually(t, serverCtx2, logger, &serverCfg2, nil)
	if addr2 != addr {
		t.Fatalf("expected restarted server to listen on %s, got %s", addr, addr2)
	}

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
