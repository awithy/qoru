package config

import "testing"

func validClientConfig() Config {
	return Config{NodeID: "client-1", Mode: ModeClient, Identity: IdentityConfig{Cert: "client.crt", Key: "client.key", CA: "ca.crt"}, Servers: []ServerConfig{{ID: "server-1", Address: "127.0.0.1:4433"}}, Forwards: []ForwardConfig{{Protocol: "tcp", Listen: "127.0.0.1:15432", Service: "echo"}}}
}

func validServerConfig() Config {
	return Config{NodeID: "server-1", Mode: ModeServer, Identity: IdentityConfig{Cert: "server.crt", Key: "server.key", CA: "ca.crt"}, Listen: "127.0.0.1:4433", Services: []ServiceConfig{{Name: "echo", Protocol: "tcp", Target: "127.0.0.1:9000", Peers: []string{"client-1"}}}}
}

func TestValidateClientAcceptsValidConfig(t *testing.T) {
	cfg := validClientConfig()
	if err := ValidateClient(&cfg); err != nil {
		t.Fatalf("expected valid client config, got %v", err)
	}
}
func TestValidateClientRejectsWrongMode(t *testing.T) {
	cfg := validClientConfig()
	cfg.Mode = ModeServer
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected wrong mode to be rejected")
	}
}
func TestValidateClientRejectsDuplicateServers(t *testing.T) {
	cfg := validClientConfig()
	cfg.Servers = []ServerConfig{{ID: "server-1", Address: "127.0.0.1:4433"}, {ID: "server-1", Address: "127.0.0.1:4434"}}
	cfg.Forwards[0].Egress = "server-1"
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected duplicate servers to be rejected")
	}
}

func TestValidateClientRejectsMissingEgressWithMultipleServers(t *testing.T) {
	cfg := validClientConfig()
	cfg.Servers = []ServerConfig{{ID: "server-1", Address: "127.0.0.1:4433"}, {ID: "server-2", Address: "127.0.0.1:4434"}}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected missing egress with multiple servers to be rejected")
	}
}

func TestValidateClientRejectsUnknownEgress(t *testing.T) {
	cfg := validClientConfig()
	cfg.Servers = []ServerConfig{{ID: "server-1", Address: "127.0.0.1:4433"}, {ID: "server-2", Address: "127.0.0.1:4434"}}
	cfg.Forwards[0].Egress = "server-3"
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected unknown egress to be rejected")
	}
}

func TestValidateClientAcceptsOneHopRoute(t *testing.T) {
	cfg := validClientConfig()
	cfg.Forwards[0].Egress = "server-1"
	cfg.Forwards[0].Route = []string{"server-1"}
	if err := ValidateClient(&cfg); err != nil {
		t.Fatalf("expected one-hop route to be accepted, got %v", err)
	}
}

func TestValidateClientRejectsEmptyRouteHop(t *testing.T) {
	cfg := validClientConfig()
	cfg.Forwards[0].Route = []string{""}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected empty route hop to be rejected")
	}
}

func TestValidateClientRejectsUnknownFirstRouteHop(t *testing.T) {
	cfg := validClientConfig()
	cfg.Forwards[0].Route = []string{"server-2"}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected unknown first route hop to be rejected")
	}
}

func TestValidateClientRejectsEgressRouteMismatch(t *testing.T) {
	cfg := validClientConfig()
	cfg.Servers = []ServerConfig{{ID: "server-1", Address: "127.0.0.1:4433"}, {ID: "server-2", Address: "127.0.0.1:4434"}}
	cfg.Forwards[0].Egress = "server-2"
	cfg.Forwards[0].Route = []string{"server-1"}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected egress/route mismatch to be rejected")
	}
}

func TestValidateClientAcceptsMultiHopRoute(t *testing.T) {
	cfg := validClientConfig()
	cfg.Forwards[0].Egress = "server-2"
	cfg.Forwards[0].Route = []string{"server-1", "server-2"}
	if err := ValidateClient(&cfg); err != nil {
		t.Fatalf("expected multi-hop route to be accepted, got %v", err)
	}
}

func TestValidateClientAcceptsStaticServiceRoute(t *testing.T) {
	cfg := validClientConfig()
	cfg.Servers = []ServerConfig{{ID: "relay-a", Address: "127.0.0.1:4433"}}
	cfg.Routes = []ServiceRouteConfig{{
		Service:   "echo",
		Protocol:  "tcp",
		Selection: RouteSelectionOrdered,
		Candidates: []RouteCandidateConfig{{
			Egress: "relay-b",
			Route:  []string{"relay-a", "relay-b"},
		}},
	}}
	cfg.Forwards[0].Egress = ""
	if err := ValidateClient(&cfg); err != nil {
		t.Fatalf("expected static service route to be accepted, got %v", err)
	}
}

func TestValidateClientRejectsStaticServiceRouteMissingService(t *testing.T) {
	cfg := validClientConfig()
	cfg.Routes = []ServiceRouteConfig{{Protocol: "tcp", Candidates: []RouteCandidateConfig{{Egress: "server-1", Route: []string{"server-1"}}}}}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected static service route missing service to be rejected")
	}
}

func TestValidateClientRejectsStaticServiceRouteUnsupportedProtocol(t *testing.T) {
	cfg := validClientConfig()
	cfg.Routes = []ServiceRouteConfig{{Service: "echo", Protocol: "udp", Candidates: []RouteCandidateConfig{{Egress: "server-1", Route: []string{"server-1"}}}}}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected static service route unsupported protocol to be rejected")
	}
}

func TestValidateClientRejectsStaticServiceRouteUnsupportedSelection(t *testing.T) {
	cfg := validClientConfig()
	cfg.Routes = []ServiceRouteConfig{{Service: "echo", Protocol: "tcp", Selection: "round_robin", Candidates: []RouteCandidateConfig{{Egress: "server-1", Route: []string{"server-1"}}}}}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected static service route unsupported selection to be rejected")
	}
}

func TestValidateClientRejectsStaticServiceRouteMissingCandidate(t *testing.T) {
	cfg := validClientConfig()
	cfg.Routes = []ServiceRouteConfig{{Service: "echo", Protocol: "tcp"}}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected static service route missing candidate to be rejected")
	}
}

func TestValidateClientRejectsStaticServiceRouteMissingCandidateEgress(t *testing.T) {
	cfg := validClientConfig()
	cfg.Routes = []ServiceRouteConfig{{Service: "echo", Protocol: "tcp", Candidates: []RouteCandidateConfig{{Route: []string{"server-1"}}}}}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected static service route missing candidate egress to be rejected")
	}
}

func TestValidateClientRejectsStaticServiceRouteEmptyCandidateRoute(t *testing.T) {
	cfg := validClientConfig()
	cfg.Routes = []ServiceRouteConfig{{Service: "echo", Protocol: "tcp", Candidates: []RouteCandidateConfig{{Egress: "server-1"}}}}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected static service route empty candidate route to be rejected")
	}
}

func TestValidateClientRejectsStaticServiceRouteUnknownFirstHop(t *testing.T) {
	cfg := validClientConfig()
	cfg.Routes = []ServiceRouteConfig{{Service: "echo", Protocol: "tcp", Candidates: []RouteCandidateConfig{{Egress: "server-2", Route: []string{"server-2"}}}}}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected static service route unknown first hop to be rejected")
	}
}

func TestValidateClientRejectsStaticServiceRouteEgressMismatch(t *testing.T) {
	cfg := validClientConfig()
	cfg.Routes = []ServiceRouteConfig{{Service: "echo", Protocol: "tcp", Candidates: []RouteCandidateConfig{{Egress: "server-2", Route: []string{"server-1"}}}}}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected static service route egress mismatch to be rejected")
	}
}

func TestValidateClientRejectsDuplicateStaticServiceRoutes(t *testing.T) {
	cfg := validClientConfig()
	cfg.Routes = []ServiceRouteConfig{
		{Service: "echo", Protocol: "tcp", Candidates: []RouteCandidateConfig{{Egress: "server-1", Route: []string{"server-1"}}}},
		{Service: "echo", Protocol: "tcp", Candidates: []RouteCandidateConfig{{Egress: "server-1", Route: []string{"server-1"}}}},
	}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected duplicate static service routes to be rejected")
	}
}

func TestValidateClientRejectsMissingForward(t *testing.T) {
	cfg := validClientConfig()
	cfg.Forwards = nil
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected missing forward to be rejected")
	}
}
func TestValidateClientRejectsMissingForwardProtocol(t *testing.T) {
	cfg := validClientConfig()
	cfg.Forwards[0].Protocol = ""
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected missing forward protocol to be rejected")
	}
}
func TestValidateClientRejectsUnsupportedForwardProtocol(t *testing.T) {
	cfg := validClientConfig()
	cfg.Forwards[0].Protocol = "udp"
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected unsupported forward protocol to be rejected")
	}
}
func TestValidateClientRejectsMissingForwardService(t *testing.T) {
	cfg := validClientConfig()
	cfg.Forwards[0].Service = ""
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected missing forward service to be rejected")
	}
}
func TestValidateServerAcceptsValidConfig(t *testing.T) {
	cfg := validServerConfig()
	if err := ValidateServer(&cfg); err != nil {
		t.Fatalf("expected valid server config, got %v", err)
	}
}
func TestValidateServerRejectsMissingListen(t *testing.T) {
	cfg := validServerConfig()
	cfg.Listen = ""
	if err := ValidateServer(&cfg); err == nil {
		t.Fatal("expected missing listen to be rejected")
	}
}

func TestValidateServerAcceptsPeer(t *testing.T) {
	cfg := validServerConfig()
	cfg.Peers = []PeerConfig{{ID: "relay-b", Address: "127.0.0.1:4434", Dial: true}}
	if err := ValidateServer(&cfg); err != nil {
		t.Fatalf("expected server peer to be accepted, got %v", err)
	}
}

func TestValidateServerRejectsServers(t *testing.T) {
	cfg := validServerConfig()
	cfg.Servers = []ServerConfig{{ID: "relay-b", Address: "127.0.0.1:4434"}}
	if err := ValidateServer(&cfg); err == nil {
		t.Fatal("expected server-mode servers to be rejected")
	}
}

func TestValidateServerRejectsRoutes(t *testing.T) {
	cfg := validServerConfig()
	cfg.Routes = []ServiceRouteConfig{{Service: "echo", Protocol: "tcp", Candidates: []RouteCandidateConfig{{Egress: "relay-b", Route: []string{"relay-b"}}}}}
	if err := ValidateServer(&cfg); err == nil {
		t.Fatal("expected server-mode routes to be rejected")
	}
}

func TestValidateServerRejectsPeerDialWithoutAddress(t *testing.T) {
	cfg := validServerConfig()
	cfg.Peers = []PeerConfig{{ID: "relay-b", Dial: true}}
	if err := ValidateServer(&cfg); err == nil {
		t.Fatal("expected peer dial without address to be rejected")
	}
}
func TestValidateServerRejectsInvalidServiceProtocol(t *testing.T) {
	cfg := validServerConfig()
	cfg.Services[0].Protocol = "icmp"
	if err := ValidateServer(&cfg); err == nil {
		t.Fatal("expected invalid service protocol to be rejected")
	}
}
func TestValidateServerRejectsInvalidServiceTarget(t *testing.T) {
	cfg := validServerConfig()
	cfg.Services[0].Target = "localhost"
	if err := ValidateServer(&cfg); err == nil {
		t.Fatal("expected invalid service target to be rejected")
	}
}
func TestValidateServerRejectsEmptyServicePeer(t *testing.T) {
	cfg := validServerConfig()
	cfg.Services[0].Peers = []string{""}
	if err := ValidateServer(&cfg); err == nil {
		t.Fatal("expected empty service peer to be rejected")
	}
}
func TestValidateForMode(t *testing.T) {
	client := validClientConfig()
	server := validServerConfig()
	if err := ValidateForMode(&client); err != nil {
		t.Fatalf("expected valid client config, got %v", err)
	}
	if err := ValidateForMode(&server); err != nil {
		t.Fatalf("expected valid server config, got %v", err)
	}
	server.Mode = "wat"
	if err := ValidateForMode(&server); err == nil {
		t.Fatal("expected invalid mode to be rejected")
	}
}
