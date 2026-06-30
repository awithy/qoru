package config

import (
	"fmt"
	"net"
)

const (
	MaxForwardRouteLength = 3
	RouteSelectionOrdered = "ordered"
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
	servers, err := validateClientServers(cfg)
	if err != nil {
		return err
	}
	if err := validateServiceRoutes(cfg, servers); err != nil {
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
					if !hasServiceRoute(cfg, fwd.Protocol, fwd.Service) {
						return fmt.Errorf("forwards[%d].egress is required when multiple servers are configured unless a static service route is configured", i)
					}
				} else if _, ok := servers[fwd.Egress]; !ok {
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

func hasServiceRoute(cfg *Config, protocol, service string) bool {
	for _, route := range cfg.Routes {
		if route.Protocol == protocol && route.Service == service {
			return true
		}
	}
	return false
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

func validateServiceRoutes(cfg *Config, servers map[string]ServerConfig) error {
	seen := make(map[string]struct{}, len(cfg.Routes))
	for i, route := range cfg.Routes {
		if route.Service == "" {
			return fmt.Errorf("routes[%d].service is required", i)
		}
		if route.Protocol == "" {
			return fmt.Errorf("routes[%d].protocol is required", i)
		}
		if route.Protocol != "tcp" {
			return fmt.Errorf("routes[%d].protocol must be tcp", i)
		}
		selection := route.Selection
		if selection == "" {
			selection = RouteSelectionOrdered
		}
		if selection != RouteSelectionOrdered {
			return fmt.Errorf("routes[%d].selection must be %q", i, RouteSelectionOrdered)
		}
		key := route.Protocol + "\x00" + route.Service
		if _, exists := seen[key]; exists {
			return fmt.Errorf("routes[%d] duplicates %s service %q", i, route.Protocol, route.Service)
		}
		seen[key] = struct{}{}
		if len(route.Candidates) == 0 {
			return fmt.Errorf("routes[%d].candidates must contain at least one entry", i)
		}
		for j, candidate := range route.Candidates {
			if candidate.Egress == "" {
				return fmt.Errorf("routes[%d].candidates[%d].egress is required", i, j)
			}
			if len(candidate.Route) == 0 {
				return fmt.Errorf("routes[%d].candidates[%d].route must contain at least one hop", i, j)
			}
			if len(candidate.Route) > MaxForwardRouteLength {
				return fmt.Errorf("routes[%d].candidates[%d].route has %d hops, max is %d", i, j, len(candidate.Route), MaxForwardRouteLength)
			}
			for k, hop := range candidate.Route {
				if hop == "" {
					return fmt.Errorf("routes[%d].candidates[%d].route[%d] is required", i, j, k)
				}
			}
			if _, ok := servers[candidate.Route[0]]; !ok {
				return fmt.Errorf("routes[%d].candidates[%d].route[0] %q does not match a configured server", i, j, candidate.Route[0])
			}
			if candidate.Route[len(candidate.Route)-1] != candidate.Egress {
				return fmt.Errorf("routes[%d].candidates[%d].egress %q must match final route hop %q", i, j, candidate.Egress, candidate.Route[len(candidate.Route)-1])
			}
		}
	}
	return nil
}

func validateServerPeers(cfg *Config) (map[string]PeerConfig, error) {
	peers := make(map[string]PeerConfig)
	for i, peer := range cfg.Peers {
		if peer.ID == "" {
			return nil, fmt.Errorf("peers[%d].id is required", i)
		}
		if peer.ID == cfg.NodeID {
			return nil, fmt.Errorf("peers[%d].id must not match node_id", i)
		}
		if peer.Address != "" {
			if _, _, err := net.SplitHostPort(peer.Address); err != nil {
				return nil, fmt.Errorf("peers[%d].address must be host:port: %w", i, err)
			}
		}
		if peer.Dial && peer.Address == "" {
			return nil, fmt.Errorf("peers[%d].address is required when dial is true", i)
		}
		if _, exists := peers[peer.ID]; exists {
			return nil, fmt.Errorf("peers[%d].id %q is duplicated", i, peer.ID)
		}
		peers[peer.ID] = peer
	}
	return peers, nil
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
	if len(cfg.Routes) > 0 {
		return fmt.Errorf("routes is client-mode only")
	}
	if len(cfg.Servers) > 0 {
		return fmt.Errorf("servers is client-mode only; use peers for server relay configuration")
	}
	if _, err := validateServerPeers(cfg); err != nil {
		return err
	}
	for i, client := range cfg.AllowedRelayClients {
		if client == "" {
			return fmt.Errorf("allowed_relay_clients[%d] is required", i)
		}
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
		if err := validateServiceE2E(cfg, i, svc); err != nil {
			return err
		}
		for j, peer := range svc.Peers {
			if peer == "" {
				return fmt.Errorf("services[%d].peers[%d] is required", i, j)
			}
		}
	}
	return nil
}

func validateServiceE2E(cfg *Config, i int, svc ServiceConfig) error {
	hasCert := svc.E2E.Cert != ""
	hasKey := svc.E2E.Key != ""
	if !hasCert && !hasKey {
		return nil
	}
	if svc.Protocol != "tcp" {
		return fmt.Errorf("services[%d].e2e is only supported for tcp services", i)
	}
	if !hasCert {
		return fmt.Errorf("services[%d].e2e.cert is required when e2e is configured", i)
	}
	if !hasKey {
		return fmt.Errorf("services[%d].e2e.key is required when e2e is configured", i)
	}
	if cfg.ServiceIdentity.CA == "" {
		return fmt.Errorf("service_identity.ca is required when services[%d].e2e is configured", i)
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
