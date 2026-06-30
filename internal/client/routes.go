package client

import "github.com/awithy/qoru/internal/config"

type selectedRoute struct {
	service string
	egress  string
	route   []string
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

func (r *routeResolver) resolve(forward config.ForwardConfig) selectedRoute {
	selected := selectedRoute{service: forward.Service, egress: forward.Egress, route: copyRoute(forward.Route)}
	if len(forward.Route) > 0 || forward.Egress != "" || r == nil {
		return selected
	}

	serviceRoute, ok := r.routes[routeKey{protocol: forward.Protocol, service: forward.Service}]
	if !ok || len(serviceRoute.Candidates) == 0 {
		return selected
	}

	candidate := serviceRoute.Candidates[0]
	return selectedRoute{service: forward.Service, egress: candidate.Egress, route: copyRoute(candidate.Route)}
}

func copyRoute(route []string) []string {
	if len(route) == 0 {
		return nil
	}
	return append([]string(nil), route...)
}
