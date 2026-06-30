package client

import (
	"reflect"
	"testing"

	"github.com/awithy/qoru/internal/config"
)

func TestRouteResolverSelectsFirstStaticCandidate(t *testing.T) {
	resolver := newRouteResolver(&config.Config{Routes: []config.ServiceRouteConfig{{
		Service:  "echo",
		Protocol: "tcp",
		Candidates: []config.RouteCandidateConfig{
			{Egress: "relay-b", Route: []string{"relay-a", "relay-b"}},
			{Egress: "relay-c", Route: []string{"relay-a", "relay-c"}},
		},
	}}})

	selected := resolver.resolve(config.ForwardConfig{Protocol: "tcp", Service: "echo"})
	assertSelectedRoute(t, selected, "echo", "relay-b", []string{"relay-a", "relay-b"})
}

func TestRouteResolverExplicitRouteWinsOverStaticRoute(t *testing.T) {
	resolver := newRouteResolver(&config.Config{Routes: []config.ServiceRouteConfig{{
		Service:    "echo",
		Protocol:   "tcp",
		Candidates: []config.RouteCandidateConfig{{Egress: "relay-b", Route: []string{"relay-a", "relay-b"}}},
	}}})

	selected := resolver.resolve(config.ForwardConfig{Protocol: "tcp", Service: "echo", Egress: "relay-d", Route: []string{"relay-a", "relay-d"}})
	assertSelectedRoute(t, selected, "echo", "relay-d", []string{"relay-a", "relay-d"})
}

func TestRouteResolverExplicitEgressWinsOverStaticRoute(t *testing.T) {
	resolver := newRouteResolver(&config.Config{Routes: []config.ServiceRouteConfig{{
		Service:    "echo",
		Protocol:   "tcp",
		Candidates: []config.RouteCandidateConfig{{Egress: "relay-b", Route: []string{"relay-a", "relay-b"}}},
	}}})

	selected := resolver.resolve(config.ForwardConfig{Protocol: "tcp", Service: "echo", Egress: "server-1"})
	assertSelectedRoute(t, selected, "echo", "server-1", nil)
}

func TestRouteResolverFallsBackToForwardWhenNoStaticRouteMatches(t *testing.T) {
	resolver := newRouteResolver(&config.Config{Routes: []config.ServiceRouteConfig{{
		Service:    "echo-b",
		Protocol:   "tcp",
		Candidates: []config.RouteCandidateConfig{{Egress: "relay-b", Route: []string{"relay-a", "relay-b"}}},
	}}})

	selected := resolver.resolve(config.ForwardConfig{Protocol: "tcp", Service: "echo-a"})
	assertSelectedRoute(t, selected, "echo-a", "", nil)
}

func assertSelectedRoute(t *testing.T, selected selectedRoute, service, egress string, route []string) {
	t.Helper()
	if selected.service != service || selected.egress != egress || !reflect.DeepEqual(selected.route, route) {
		t.Fatalf("unexpected selected route: %#v", selected)
	}
}
