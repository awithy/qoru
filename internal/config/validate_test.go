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

func TestValidateClientRejectsMultiHopRouteForNow(t *testing.T) {
	cfg := validClientConfig()
	cfg.Forwards[0].Route = []string{"server-1", "server-2"}
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected multi-hop route to be rejected until implemented")
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
