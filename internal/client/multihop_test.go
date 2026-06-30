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
	"time"
)

func TestRunProxiesExplicitMultiHopRoute(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetListener := startEchoTCPServer(t)

	requestedAtRelay := make(chan protocol.ConnectRequest, 1)
	requestedAtEgress := make(chan protocol.ConnectRequest, 1)

	egressCfg := &config.Config{
		NodeID:   "relay-b",
		Mode:     config.ModeServer,
		Identity: makeDevNodeCert(t, "relay-b"),
		Listen:   "127.0.0.1:0",
		Peers:    []config.PeerConfig{{ID: "relay-a"}},
		Services: []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetListener.Addr().String(), Peers: []string{"relay-a"}}},
	}
	egressAddr, egressErr := startTestServerWithConfig(t, ctx, logger, egressCfg, func(req protocol.ConnectRequest) { requestedAtEgress <- req })

	relayCfg := &config.Config{
		NodeID:              "relay-a",
		Mode:                config.ModeServer,
		Identity:            makeDevNodeCert(t, "relay-a"),
		Listen:              "127.0.0.1:0",
		Peers:               []config.PeerConfig{{ID: "relay-b", Address: egressAddr, Dial: true}},
		AllowedRelayClients: []string{"client-1"},
	}
	relayAddr, relayErr := startTestServerWithConfig(t, ctx, logger, relayCfg, func(req protocol.ConnectRequest) { requestedAtRelay <- req })

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	clientCfg := testClientConfig(relayAddr)
	clientCfg.Servers = []config.ServerConfig{{ID: "relay-a", Address: relayAddr}}
	clientCfg.Forwards[0].Listen = "127.0.0.1:0"
	clientCfg.Forwards[0].Egress = "relay-b"
	clientCfg.Forwards[0].Route = []string{"relay-a", "relay-b"}

	clientStarted := make(chan string, 1)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { clientStarted <- addr }))
	}()

	clientAddr := waitForClientAddr(t, clientStarted, clientErr)
	assertEcho(t, clientAddr, "hop!")

	relayReq := waitForConnectRequest(t, requestedAtRelay)
	egressReq := waitForConnectRequest(t, requestedAtEgress)
	if relayReq.RequestID == "" {
		t.Fatal("expected relay request id to be set")
	}
	if egressReq.RequestID != relayReq.RequestID {
		t.Fatalf("expected request id to be forwarded unchanged, relay=%q egress=%q", relayReq.RequestID, egressReq.RequestID)
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
	cancelAndWaitForServer(t, cancel, relayErr)
	cancelAndWaitForServer(t, func() {}, egressErr)
}

func TestOpenTCPStreamRejectsUnauthorizedRelayIngress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetListener := startEchoTCPServer(t)

	egressCfg := &config.Config{
		NodeID:   "relay-b",
		Mode:     config.ModeServer,
		Identity: makeDevNodeCert(t, "relay-b"),
		Listen:   "127.0.0.1:0",
		Peers:    []config.PeerConfig{{ID: "relay-a"}},
		Services: []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetListener.Addr().String(), Peers: []string{"relay-a"}}},
	}
	egressAddr, egressErr := startTestServerWithConfig(t, ctx, logger, egressCfg, nil)

	relayCfg := &config.Config{
		NodeID:              "relay-a",
		Mode:                config.ModeServer,
		Identity:            makeDevNodeCert(t, "relay-a"),
		Listen:              "127.0.0.1:0",
		Peers:               []config.PeerConfig{{ID: "relay-b", Address: egressAddr, Dial: true}},
		AllowedRelayClients: []string{"client-2"},
	}
	relayAddr, relayErr := startTestServerWithConfig(t, ctx, logger, relayCfg, nil)

	clientCfg := testClientConfig(relayAddr)
	clientCfg.Servers = []config.ServerConfig{{ID: "relay-a", Address: relayAddr}}
	conn, err := connectTestClient(ctx, clientCfg, logger)
	if err != nil {
		t.Fatalf("expected client to connect: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	_, err = OpenTCPStream(ctx, conn, "018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a1234", "echo", "relay-b", []string{"relay-a", "relay-b"}, false)
	if err == nil {
		t.Fatal("expected relay ingress authorization error")
	}
	var rejected *ConnectRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("expected ConnectRejectedError, got %T: %v", err, err)
	}
	if rejected.Code != protocol.ConnectCodeAccessDenied {
		t.Fatalf("expected ACCESS_DENIED, got %s", rejected.Code)
	}
	if !strings.Contains(rejected.Message, "not allowed to use this node as a relay") {
		t.Fatalf("unexpected rejection message: %q", rejected.Message)
	}

	cancelAndWaitForServer(t, cancel, relayErr)
	cancelAndWaitForServer(t, func() {}, egressErr)
}

func TestRunProxiesUsingInboundPeerSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetListener := startEchoTCPServer(t)

	relayACfg := &config.Config{
		NodeID:              "relay-a",
		Mode:                config.ModeServer,
		Identity:            makeDevNodeCert(t, "relay-a"),
		Listen:              "127.0.0.1:0",
		Peers:               []config.PeerConfig{{ID: "relay-b"}},
		AllowedRelayClients: []string{"client-1"},
	}
	relayAAddr, relayAErr := startTestServerWithConfig(t, ctx, logger, relayACfg, nil)

	relayBCfg := &config.Config{
		NodeID:   "relay-b",
		Mode:     config.ModeServer,
		Identity: makeDevNodeCert(t, "relay-b"),
		Listen:   "127.0.0.1:0",
		Peers:    []config.PeerConfig{{ID: "relay-a", Address: relayAAddr, Dial: true}},
		Services: []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetListener.Addr().String(), Peers: []string{"relay-a"}}},
	}
	_, relayBErr := startTestServerWithConfig(t, ctx, logger, relayBCfg, nil)
	time.Sleep(200 * time.Millisecond)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	clientCfg := testClientConfig(relayAAddr)
	clientCfg.Servers = []config.ServerConfig{{ID: "relay-a", Address: relayAAddr}}
	clientCfg.Forwards[0].Listen = "127.0.0.1:0"
	clientCfg.Forwards[0].Egress = "relay-b"
	clientCfg.Forwards[0].Route = []string{"relay-a", "relay-b"}

	clientStarted := make(chan string, 1)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { clientStarted <- addr }))
	}()

	clientAddr := waitForClientAddr(t, clientStarted, clientErr)
	assertEcho(t, clientAddr, "back!")

	clientCancel()
	select {
	case err := <-clientErr:
		if err != nil {
			t.Fatalf("expected clean client shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client shutdown")
	}
	cancelAndWaitForServer(t, cancel, relayAErr)
	cancelAndWaitForServer(t, func() {}, relayBErr)
}
