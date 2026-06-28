package config

import "fmt"

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
	if len(cfg.TCPForwards) == 0 {
		return fmt.Errorf("at least one tcp_forwards entry is required for client mode")
	}
	for i, fwd := range cfg.TCPForwards {
		if fwd.Listen == "" {
			return fmt.Errorf("tcp_forwards[%d].listen is required", i)
		}
		if fwd.Target == "" {
			return fmt.Errorf("tcp_forwards[%d].target is required", i)
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
