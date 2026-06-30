package client

import (
	"context"
	"errors"
	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/awithy/qoru/internal/testcert"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestRunProxiesE2EEncryptedOneHop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	target := startEchoTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: testcert.ServiceCAPath(t)}
	serverCfg.Services = []config.ServiceConfig{{
		Name:     "echo",
		Protocol: "tcp",
		Target:   target.Addr().String(),
		Peers:    []string{"client-1"},
		E2E:      testcert.ServiceE2E(t, "relay-b-echo", "echo"),
	}}
	received := make(chan protocol.ConnectRequest, 1)
	addr, serverErr := startTestServerWithConfig(t, serverCtx, logger, serverCfg, func(req protocol.ConnectRequest) { received <- req })

	clientCfg := testClientConfig(addr)
	clientCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: testcert.ServiceCAPath(t)}
	clientCfg.Forwards[0].Listen = "127.0.0.1:0"
	clientCfg.Forwards[0].E2E = config.ForwardE2EAlways

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	started := make(chan string, 1)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { started <- addr }))
	}()
	clientAddr := waitForClientAddr(t, started, clientErr)

	assertEcho(t, clientAddr, "secret-ping")
	req := waitForConnectRequest(t, received)
	if !req.E2ERequired {
		t.Fatal("expected e2e_required connect request")
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
	cancelAndWaitForServer(t, cancel, serverErr)
}

func TestRunAllowsPlaintextOneHopForAutoE2E(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	target := startEchoTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: testcert.ServiceCAPath(t)}
	serverCfg.Services = []config.ServiceConfig{{
		Name:     "echo",
		Protocol: "tcp",
		Target:   target.Addr().String(),
		Peers:    []string{"client-1"},
		E2E:      testcert.ServiceE2E(t, "relay-b-echo", "echo"),
	}}
	received := make(chan protocol.ConnectRequest, 1)
	addr, serverErr := startTestServerWithConfig(t, serverCtx, logger, serverCfg, func(req protocol.ConnectRequest) { received <- req })

	clientCfg := testClientConfig(addr)
	clientCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: testcert.ServiceCAPath(t)}
	clientCfg.Forwards[0].Listen = "127.0.0.1:0"
	clientCfg.Forwards[0].E2E = config.ForwardE2EAuto

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	started := make(chan string, 1)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { started <- addr }))
	}()
	clientAddr := waitForClientAddr(t, started, clientErr)

	assertEcho(t, clientAddr, "direct-ping")
	req := waitForConnectRequest(t, received)
	if req.E2ERequired {
		t.Fatal("expected auto e2e to skip e2e for one-hop direct request")
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
	cancelAndWaitForServer(t, cancel, serverErr)
}

func TestOpenTCPStreamRejectsRoutedPlaintextForE2EService(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	target := startEchoTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: testcert.ServiceCAPath(t)}
	serverCfg.Peers = []config.PeerConfig{{ID: "client-1"}}
	serverCfg.Services = []config.ServiceConfig{{
		Name:     "echo",
		Protocol: "tcp",
		Target:   target.Addr().String(),
		Peers:    []string{"client-1"},
		E2E:      testcert.ServiceE2E(t, "relay-b-echo", "echo"),
	}}
	addr, serverErr := startTestServerWithConfig(t, ctx, logger, serverCfg, nil)

	clientCfg := testClientConfig(addr)
	conn, err := connectTestClient(ctx, clientCfg, logger)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	_, err = OpenTCPStream(ctx, conn, "018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a1234", "echo", "server-1", []string{"server-1"}, false)
	var rejected *ConnectRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("expected ConnectRejectedError, got %T: %v", err, err)
	}
	if rejected.Code != protocol.ConnectCodeAccessDenied {
		t.Fatalf("expected access denied, got %s", rejected.Code)
	}

	cancelAndWaitForServer(t, cancel, serverErr)
}

func TestRunProxiesE2EEncryptedMultiHopRoute(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetListener := startEchoTCPServer(t)

	requestedAtRelay := make(chan protocol.ConnectRequest, 1)
	requestedAtEgress := make(chan protocol.ConnectRequest, 1)

	egressCfg := &config.Config{
		NodeID:          "relay-b",
		Mode:            config.ModeServer,
		Identity:        makeDevNodeCert(t, "relay-b"),
		ServiceIdentity: config.ServiceIdentityConfig{CA: testcert.ServiceCAPath(t)},
		Listen:          "127.0.0.1:0",
		Peers:           []config.PeerConfig{{ID: "relay-a"}},
		Services: []config.ServiceConfig{{
			Name:     "echo",
			Protocol: "tcp",
			Target:   targetListener.Addr().String(),
			Peers:    []string{"client-1"},
			E2E:      testcert.ServiceE2E(t, "relay-b-echo", "echo"),
		}},
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
	clientCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: testcert.ServiceCAPath(t)}
	clientCfg.Servers = []config.ServerConfig{{ID: "relay-a", Address: relayAddr}}
	clientCfg.Forwards[0].Listen = "127.0.0.1:0"
	clientCfg.Forwards[0].Egress = "relay-b"
	clientCfg.Forwards[0].Route = []string{"relay-a", "relay-b"}
	clientCfg.Forwards[0].E2E = config.ForwardE2EAuto

	clientStarted := make(chan string, 1)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { clientStarted <- addr }))
	}()

	clientAddr := waitForClientAddr(t, clientStarted, clientErr)
	assertEcho(t, clientAddr, "secret-hop!")

	relayReq := waitForConnectRequest(t, requestedAtRelay)
	egressReq := waitForConnectRequest(t, requestedAtEgress)
	if !relayReq.E2ERequired || !egressReq.E2ERequired {
		t.Fatalf("expected e2e required at relay and egress: relay=%v egress=%v", relayReq.E2ERequired, egressReq.E2ERequired)
	}
	if strings.Join(egressReq.Route, ",") != "relay-b" {
		t.Fatalf("expected remaining egress route relay-b, got %#v", egressReq.Route)
	}
	if strings.Join(egressReq.E2ERoute, ",") != "relay-a,relay-b" {
		t.Fatalf("expected original e2e route to be preserved, got %#v", egressReq.E2ERoute)
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
