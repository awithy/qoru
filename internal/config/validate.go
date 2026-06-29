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
		if fwd.Target == "" {
			return fmt.Errorf("forwards[%d].target is required", i)
		}
		if _, _, err := net.SplitHostPort(fwd.Target); err != nil {
			return fmt.Errorf("forwards[%d].target must be host:port: %w", i, err)
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
	for i, target := range cfg.AllowedTargets {
		if target.Protocol == "" {
			return fmt.Errorf("allowed_targets[%d].protocol is required", i)
		}
		if target.Protocol != "tcp" && target.Protocol != "udp" {
			return fmt.Errorf("allowed_targets[%d].protocol must be tcp or udp", i)
		}
		if target.Address == "" {
			return fmt.Errorf("allowed_targets[%d].address is required", i)
		}
		if _, _, err := net.SplitHostPort(target.Address); err != nil {
			return fmt.Errorf("allowed_targets[%d].address must be host:port: %w", i, err)
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
