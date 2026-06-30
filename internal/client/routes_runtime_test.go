package client

import (
	"context"
	"github.com/awithy/qoru/internal/config"
	"io"
	"log/slog"
	"testing"
	"time"
)

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
