package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

func TestRunProxiesE2EEncryptedOneHop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	target := startEchoTCPServer(t)
	serverCfg := testServerConfig()
	serverCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: "../../dev/certs/service-ca.crt"}
	serverCfg.Services = []config.ServiceConfig{{
		Name:     "echo",
		Protocol: "tcp",
		Target:   target.Addr().String(),
		Peers:    []string{"client-1"},
		E2E:      config.ServiceE2EConfig{Cert: "../../dev/certs/relay-b-echo.crt", Key: "../../dev/certs/relay-b-echo.key"},
	}}
	received := make(chan protocol.ConnectRequest, 1)
	addr, serverErr := startTestServerWithConfig(t, serverCtx, logger, serverCfg, func(req protocol.ConnectRequest) { received <- req })

	clientCfg := testClientConfig(addr)
	clientCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: "../../dev/certs/service-ca.crt"}
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
	serverCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: "../../dev/certs/service-ca.crt"}
	serverCfg.Services = []config.ServiceConfig{{
		Name:     "echo",
		Protocol: "tcp",
		Target:   target.Addr().String(),
		Peers:    []string{"client-1"},
		E2E:      config.ServiceE2EConfig{Cert: "../../dev/certs/relay-b-echo.crt", Key: "../../dev/certs/relay-b-echo.key"},
	}}
	received := make(chan protocol.ConnectRequest, 1)
	addr, serverErr := startTestServerWithConfig(t, serverCtx, logger, serverCfg, func(req protocol.ConnectRequest) { received <- req })

	clientCfg := testClientConfig(addr)
	clientCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: "../../dev/certs/service-ca.crt"}
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
	serverCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: "../../dev/certs/service-ca.crt"}
	serverCfg.Peers = []config.PeerConfig{{ID: "client-1"}}
	serverCfg.Services = []config.ServiceConfig{{
		Name:     "echo",
		Protocol: "tcp",
		Target:   target.Addr().String(),
		Peers:    []string{"client-1"},
		E2E:      config.ServiceE2EConfig{Cert: "../../dev/certs/relay-b-echo.crt", Key: "../../dev/certs/relay-b-echo.key"},
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
		ServiceIdentity: config.ServiceIdentityConfig{CA: "../../dev/certs/service-ca.crt"},
		Listen:          "127.0.0.1:0",
		Peers:           []config.PeerConfig{{ID: "relay-a"}},
		Services: []config.ServiceConfig{{
			Name:     "echo",
			Protocol: "tcp",
			Target:   targetListener.Addr().String(),
			Peers:    []string{"client-1"},
			E2E:      config.ServiceE2EConfig{Cert: "../../dev/certs/relay-b-echo.crt", Key: "../../dev/certs/relay-b-echo.key"},
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
	clientCfg.ServiceIdentity = config.ServiceIdentityConfig{CA: "../../dev/certs/service-ca.crt"}
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

func TestRunRoutesForwardsToMultipleServersByEgress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetA := startFixedTCPServer(t, "aaaa")
	targetB := startFixedTCPServer(t, "bbbb")

	serverCfgA := testServerConfig()
	serverCfgA.NodeID = "server-1"
	serverCfgA.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetA.Addr().String(), Peers: []string{"client-1"}}}
	addrA, serverErrA := startTestServerWithConfig(t, ctx, logger, serverCfgA, nil)

	serverCfgB := testServerConfig()
	serverCfgB.NodeID = "server-2"
	serverCfgB.Identity = makeDevNodeCert(t, "server-2")
	serverCfgB.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetB.Addr().String(), Peers: []string{"client-1"}}}
	addrB, serverErrB := startTestServerWithConfig(t, ctx, logger, serverCfgB, nil)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	clientCfg := testClientConfig(addrA)
	clientCfg.Servers = []config.ServerConfig{{ID: "server-1", Address: addrA}, {ID: "server-2", Address: addrB}}
	clientCfg.Forwards = []config.ForwardConfig{
		{Protocol: "tcp", Listen: "127.0.0.1:0", Service: "echo", Egress: "server-1"},
		{Protocol: "tcp", Listen: "127.0.0.1:0", Service: "echo", Egress: "server-2"},
	}

	started := make(chan string, 2)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { started <- addr }))
	}()

	clientAddrA := waitForClientAddr(t, started, clientErr)
	clientAddrB := waitForClientAddr(t, started, clientErr)
	assertRead(t, clientAddrA, "aaaa")
	assertRead(t, clientAddrB, "bbbb")

	clientCancel()
	select {
	case err := <-clientErr:
		if err != nil {
			t.Fatalf("expected clean client shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client shutdown")
	}
	cancelAndWaitForServer(t, cancel, serverErrA)
	select {
	case err := <-serverErrB:
		if err != nil {
			t.Fatalf("expected clean server B shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server B shutdown")
	}
}

func TestRunUsesStaticServiceRouteCandidate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetA := startFixedTCPServer(t, "aaaa")
	targetB := startFixedTCPServer(t, "bbbb")

	serverCfgA := testServerConfig()
	serverCfgA.NodeID = "server-1"
	serverCfgA.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetA.Addr().String(), Peers: []string{"client-1"}}}
	serverCfgA.Peers = []config.PeerConfig{{ID: "client-1"}}
	addrA, serverErrA := startTestServerWithConfig(t, ctx, logger, serverCfgA, nil)

	serverCfgB := testServerConfig()
	serverCfgB.NodeID = "server-2"
	serverCfgB.Identity = makeDevNodeCert(t, "server-2")
	serverCfgB.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetB.Addr().String(), Peers: []string{"client-1"}}}
	serverCfgB.Peers = []config.PeerConfig{{ID: "client-1"}}
	addrB, serverErrB := startTestServerWithConfig(t, ctx, logger, serverCfgB, nil)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	clientCfg := testClientConfig(addrA)
	clientCfg.Servers = []config.ServerConfig{{ID: "server-1", Address: addrA}, {ID: "server-2", Address: addrB}}
	clientCfg.Routes = []config.ServiceRouteConfig{{
		Service:   "echo",
		Protocol:  "tcp",
		Selection: config.RouteSelectionOrdered,
		Candidates: []config.RouteCandidateConfig{{
			Egress: "server-2",
			Route:  []string{"server-2"},
		}},
	}}
	clientCfg.Forwards = []config.ForwardConfig{{Protocol: "tcp", Listen: "127.0.0.1:0", Service: "echo"}}

	started := make(chan string, 1)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { started <- addr }))
	}()

	clientAddr := waitForClientAddr(t, started, clientErr)
	assertRead(t, clientAddr, "bbbb")

	clientCancel()
	select {
	case err := <-clientErr:
		if err != nil {
			t.Fatalf("expected clean client shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client shutdown")
	}
	cancelAndWaitForServer(t, cancel, serverErrA)
	select {
	case err := <-serverErrB:
		if err != nil {
			t.Fatalf("expected clean server B shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server B shutdown")
	}
}

func TestRunFallsBackToNextStaticServiceRouteCandidate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetB := startFixedTCPServer(t, "bbbb")

	serverCfgA := testServerConfig()
	serverCfgA.NodeID = "server-1"
	serverCfgA.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: closedTCPAddr(t), Peers: []string{"client-1"}}}
	serverCfgA.Peers = []config.PeerConfig{{ID: "client-1"}}
	addrA, serverErrA := startTestServerWithConfig(t, ctx, logger, serverCfgA, nil)

	serverCfgB := testServerConfig()
	serverCfgB.NodeID = "server-2"
	serverCfgB.Identity = makeDevNodeCert(t, "server-2")
	serverCfgB.Services = []config.ServiceConfig{{Name: "echo", Protocol: "tcp", Target: targetB.Addr().String(), Peers: []string{"client-1"}}}
	serverCfgB.Peers = []config.PeerConfig{{ID: "client-1"}}
	addrB, serverErrB := startTestServerWithConfig(t, ctx, logger, serverCfgB, nil)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	clientCfg := testClientConfig(addrA)
	clientCfg.Servers = []config.ServerConfig{{ID: "server-1", Address: addrA}, {ID: "server-2", Address: addrB}}
	clientCfg.Routes = []config.ServiceRouteConfig{{
		Service:  "echo",
		Protocol: "tcp",
		Candidates: []config.RouteCandidateConfig{
			{Egress: "server-1", Route: []string{"server-1"}},
			{Egress: "server-2", Route: []string{"server-2"}},
		},
	}}
	clientCfg.Forwards = []config.ForwardConfig{{Protocol: "tcp", Listen: "127.0.0.1:0", Service: "echo"}}

	started := make(chan string, 1)
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- Run(clientCtx, clientCfg, logger, WithStartedFunc(func(addr string) { started <- addr }))
	}()

	clientAddr := waitForClientAddr(t, started, clientErr)
	assertRead(t, clientAddr, "bbbb")

	clientCancel()
	select {
	case err := <-clientErr:
		if err != nil {
			t.Fatalf("expected clean client shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client shutdown")
	}
	cancelAndWaitForServer(t, cancel, serverErrA)
	select {
	case err := <-serverErrB:
		if err != nil {
			t.Fatalf("expected clean server B shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server B shutdown")
	}
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
		Identity: config.IdentityConfig{Cert: "../../dev/certs/server-1.crt", Key: "../../dev/certs/server-1.key", CA: "../../dev/certs/ca.crt"},
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
	dir := t.TempDir()
	cert := filepath.Join(dir, nodeID+".crt")
	key := filepath.Join(dir, nodeID+".key")
	cnf := filepath.Join(dir, nodeID+".cnf")
	csr := filepath.Join(dir, nodeID+".csr")
	cfg := "[req]\n" +
		"distinguished_name = req_distinguished_name\n" +
		"req_extensions = v3_req\n" +
		"prompt = no\n" +
		"[req_distinguished_name]\n" +
		"CN = " + nodeID + "\n" +
		"[v3_req]\n" +
		"basicConstraints = CA:FALSE\n" +
		"keyUsage = critical,digitalSignature,keyEncipherment\n" +
		"extendedKeyUsage = serverAuth,clientAuth\n" +
		"subjectAltName = URI:spiffe://qoru/node/" + nodeID + "\n"
	if err := os.WriteFile(cnf, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	runOpenSSL(t, "genrsa", "-out", key, "2048")
	runOpenSSL(t, "req", "-new", "-key", key, "-out", csr, "-config", cnf)
	runOpenSSL(t, "x509", "-req", "-in", csr, "-CA", "../../dev/certs/ca.crt", "-CAkey", "../../dev/certs/ca.key", "-CAcreateserial", "-out", cert, "-days", "365", "-sha256", "-extensions", "v3_req", "-extfile", cnf)
	return config.IdentityConfig{Cert: cert, Key: key, CA: "../../dev/certs/ca.crt"}
}

func runOpenSSL(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("openssl", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("openssl %v failed: %v\n%s", args, err, out)
	}
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
		Identity: config.IdentityConfig{Cert: "../../dev/certs/client-1.crt", Key: "../../dev/certs/client-1.key", CA: "../../dev/certs/ca.crt"},
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
