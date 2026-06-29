package config

import (
	"fmt"
	"net"
)

const MaxForwardRouteLength = 3

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
	servers, err := validateClientServers(cfg)
	if err != nil {
		return err
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
		if err := validateForwardRoute(i, fwd, servers); err != nil {
			return err
		}
		if len(fwd.Route) == 0 {
			if len(servers) > 1 {
				if fwd.Egress == "" {
					return fmt.Errorf("forwards[%d].egress is required when multiple servers are configured", i)
				}
				if _, ok := servers[fwd.Egress]; !ok {
					return fmt.Errorf("forwards[%d].egress %q does not match a configured server", i, fwd.Egress)
				}
			} else if fwd.Egress != "" {
				if _, ok := servers[fwd.Egress]; !ok {
					return fmt.Errorf("forwards[%d].egress %q does not match the configured server", i, fwd.Egress)
				}
			}
		}
	}
	return nil
}

func validateForwardRoute(i int, fwd ForwardConfig, servers map[string]ServerConfig) error {
	if len(fwd.Route) == 0 {
		return nil
	}
	if len(fwd.Route) > MaxForwardRouteLength {
		return fmt.Errorf("forwards[%d].route has %d hops, max is %d", i, len(fwd.Route), MaxForwardRouteLength)
	}
	for j, hop := range fwd.Route {
		if hop == "" {
			return fmt.Errorf("forwards[%d].route[%d] is required", i, j)
		}
	}
	firstHop := fwd.Route[0]
	if _, ok := servers[firstHop]; !ok {
		return fmt.Errorf("forwards[%d].route[0] %q does not match a configured server", i, firstHop)
	}
	if fwd.Egress != "" && fwd.Egress != fwd.Route[len(fwd.Route)-1] {
		return fmt.Errorf("forwards[%d].egress %q must match final route hop %q", i, fwd.Egress, fwd.Route[len(fwd.Route)-1])
	}
	return nil
}

func validateClientServers(cfg *Config) (map[string]ServerConfig, error) {
	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("at least one servers entry is required for client mode")
	}
	return validateConfiguredServers(cfg)
}

func validateConfiguredServers(cfg *Config) (map[string]ServerConfig, error) {
	servers := make(map[string]ServerConfig)
	for i, server := range cfg.Servers {
		if server.ID == "" {
			return nil, fmt.Errorf("servers[%d].id is required", i)
		}
		if server.Address == "" {
			return nil, fmt.Errorf("servers[%d].address is required", i)
		}
		if _, exists := servers[server.ID]; exists {
			return nil, fmt.Errorf("servers[%d].id %q is duplicated", i, server.ID)
		}
		servers[server.ID] = server
	}
	return servers, nil
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
	if _, err := validateConfiguredServers(cfg); err != nil {
		return err
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
