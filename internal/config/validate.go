package config

import (
	"fmt"
	"net"
)

func ValidateForMode(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	switch cfg.Mode {
	case ModeClient:
		return ValidateClient(cfg)
	case ModeServer:
		return ValidateServer(cfg)
	case "":
		return fmt.Errorf("mode is required")
	default:
		return fmt.Errorf("invalid mode %q", cfg.Mode)
	}
}

func ValidateClient(cfg *Config) error {
	if err := validateCommon(cfg); err != nil {
		return err
	}
	if cfg.Mode != ModeClient {
		return fmt.Errorf("config mode %q cannot be used with client command", cfg.Mode)
	}
	if cfg.Server == nil {
		return fmt.Errorf("server is required for client mode")
	}
	if cfg.Server.ID == "" {
		return fmt.Errorf("server.id is required for client mode")
	}
	if cfg.Server.Address == "" {
		return fmt.Errorf("server.address is required for client mode")
	}
	if len(cfg.Forwards) == 0 {
		return fmt.Errorf("at least one forwards entry is required for client mode")
	}
	for i, fwd := range cfg.Forwards {
		if fwd.Protocol == "" {
			return fmt.Errorf("forwards[%d].protocol is required", i)
		}
		if fwd.Protocol != "tcp" {
			return fmt.Errorf("forwards[%d].protocol must be tcp", i)
		}
		if fwd.Listen == "" {
			return fmt.Errorf("forwards[%d].listen is required", i)
		}
		if _, _, err := net.SplitHostPort(fwd.Listen); err != nil {
			return fmt.Errorf("forwards[%d].listen must be host:port: %w", i, err)
		}
		if fwd.Service == "" {
			return fmt.Errorf("forwards[%d].service is required", i)
		}
	}
	return nil
}

func ValidateServer(cfg *Config) error {
	if err := validateCommon(cfg); err != nil {
		return err
	}
	if cfg.Mode != ModeServer {
		return fmt.Errorf("config mode %q cannot be used with server command", cfg.Mode)
	}
	if cfg.Listen == "" {
		return fmt.Errorf("listen is required for server mode")
	}
	for i, svc := range cfg.Services {
		if svc.Name == "" {
			return fmt.Errorf("services[%d].name is required", i)
		}
		if svc.Protocol == "" {
			return fmt.Errorf("services[%d].protocol is required", i)
		}
		if svc.Protocol != "tcp" && svc.Protocol != "udp" {
			return fmt.Errorf("services[%d].protocol must be tcp or udp", i)
		}
		if svc.Target == "" {
			return fmt.Errorf("services[%d].target is required", i)
		}
		if _, _, err := net.SplitHostPort(svc.Target); err != nil {
			return fmt.Errorf("services[%d].target must be host:port: %w", i, err)
		}
		for j, peer := range svc.Peers {
			if peer == "" {
				return fmt.Errorf("services[%d].peers[%d] is required", i, j)
			}
		}
	}
	return nil
}

func validateCommon(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.NodeID == "" {
		return fmt.Errorf("node_id is required")
	}
	if cfg.Mode == "" {
		return fmt.Errorf("mode is required")
	}
	if cfg.Identity.Cert == "" {
		return fmt.Errorf("identity.cert is required")
	}
	if cfg.Identity.Key == "" {
		return fmt.Errorf("identity.key is required")
	}
	if cfg.Identity.CA == "" {
		return fmt.Errorf("identity.ca is required")
	}
	return nil
}
