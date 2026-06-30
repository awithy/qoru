package client

import "github.com/awithy/qoru/internal/config"

type selectedRoute struct {
	service string
	egress  string
	route   []string
	e2eMode string
}

type routeResolver struct {
	routes map[routeKey]config.ServiceRouteConfig
}

type routeKey struct {
	protocol string
	service  string
}

func newRouteResolver(cfg *config.Config) *routeResolver {
	routes := make(map[routeKey]config.ServiceRouteConfig, len(cfg.Routes))
	for _, route := range cfg.Routes {
		routes[routeKey{protocol: route.Protocol, service: route.Service}] = route
	}
	return &routeResolver{routes: routes}
}

func (r *routeResolver) resolveCandidates(forward config.ForwardConfig) []selectedRoute {
	selected := selectedRoute{service: forward.Service, egress: forward.Egress, route: copyRoute(forward.Route), e2eMode: normalizedForwardE2E(forward.E2E)}
	if len(forward.Route) > 0 || forward.Egress != "" || r == nil {
		return []selectedRoute{selected}
	}

	serviceRoute, ok := r.routes[routeKey{protocol: forward.Protocol, service: forward.Service}]
	if !ok || len(serviceRoute.Candidates) == 0 {
		return []selectedRoute{selected}
	}

	candidates := make([]selectedRoute, 0, len(serviceRoute.Candidates))
	for _, candidate := range serviceRoute.Candidates {
		candidates = append(candidates, selectedRoute{service: forward.Service, egress: candidate.Egress, route: copyRoute(candidate.Route), e2eMode: normalizedForwardE2E(forward.E2E)})
	}
	return candidates
}

func normalizedForwardE2E(mode string) string {
	if mode == "" {
		return config.ForwardE2EOff
	}
	return mode
}

func (r selectedRoute) effectiveE2ERequired() bool {
	switch r.e2eMode {
	case config.ForwardE2EAlways:
		return true
	case config.ForwardE2EAuto:
		return len(r.route) > 1
	default:
		return false
	}
}

func copyRoute(route []string) []string {
	if len(route) == 0 {
		return nil
	}
	return append([]string(nil), route...)
}
