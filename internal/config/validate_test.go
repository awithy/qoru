package config

import "testing"

func validClientConfig() Config {
	return Config{
		NodeID:      "client-1",
		Mode:        ModeClient,
		Identity:    IdentityConfig{Cert: "client.crt", Key: "client.key", CA: "ca.crt"},
		Server:      &ServerConfig{ID: "server-1", Address: "127.0.0.1:4433"},
		TCPForwards: []TCPForwardConfig{{Listen: "127.0.0.1:15432", Target: "127.0.0.1:5432"}},
	}
}

func validServerConfig() Config {
	return Config{
		NodeID:   "server-1",
		Mode:     ModeServer,
		Identity: IdentityConfig{Cert: "server.crt", Key: "server.key", CA: "ca.crt"},
		Listen:   "127.0.0.1:4433",
	}
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

func TestValidateClientRejectsMissingForward(t *testing.T) {
	cfg := validClientConfig()
	cfg.TCPForwards = nil
	if err := ValidateClient(&cfg); err == nil {
		t.Fatal("expected missing forward to be rejected")
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

func TestValidateServerAcceptsAllowedTargets(t *testing.T) {
	cfg := validServerConfig()
	cfg.AllowedTargets = []AllowedTargetConfig{
		{Protocol: "tcp", Address: "127.0.0.1:9000"},
		{Protocol: "udp", Address: "127.0.0.1:5353"},
	}
	if err := ValidateServer(&cfg); err != nil {
		t.Fatalf("expected valid server config, got %v", err)
	}
}

func TestValidateServerRejectsInvalidAllowedTargetProtocol(t *testing.T) {
	cfg := validServerConfig()
	cfg.AllowedTargets = []AllowedTargetConfig{{Protocol: "icmp", Address: "127.0.0.1:9000"}}
	if err := ValidateServer(&cfg); err == nil {
		t.Fatal("expected invalid allowed target protocol to be rejected")
	}
}

func TestValidateServerRejectsInvalidAllowedTargetAddress(t *testing.T) {
	cfg := validServerConfig()
	cfg.AllowedTargets = []AllowedTargetConfig{{Protocol: "tcp", Address: "localhost"}}
	if err := ValidateServer(&cfg); err == nil {
		t.Fatal("expected invalid allowed target address to be rejected")
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
