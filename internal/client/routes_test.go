package client

import (
	"errors"
	"reflect"
	"testing"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/protocol"
)

func TestRouteResolverSelectsStaticCandidates(t *testing.T) {
	resolver := newRouteResolver(&config.Config{Routes: []config.ServiceRouteConfig{{
		Service:  "echo",
		Protocol: "tcp",
		Candidates: []config.RouteCandidateConfig{
			{Egress: "relay-b", Route: []string{"relay-a", "relay-b"}},
			{Egress: "relay-c", Route: []string{"relay-a", "relay-c"}},
		},
	}}})

	candidates := resolver.resolveCandidates(config.ForwardConfig{Protocol: "tcp", Service: "echo"})
	assertRouteCandidates(t, candidates,
		selectedRoute{service: "echo", egress: "relay-b", route: []string{"relay-a", "relay-b"}, e2eMode: config.ForwardE2EOff},
		selectedRoute{service: "echo", egress: "relay-c", route: []string{"relay-a", "relay-c"}, e2eMode: config.ForwardE2EOff},
	)
}

func TestRouteResolverExplicitRouteWinsOverStaticRoute(t *testing.T) {
	resolver := newRouteResolver(&config.Config{Routes: []config.ServiceRouteConfig{{
		Service:    "echo",
		Protocol:   "tcp",
		Candidates: []config.RouteCandidateConfig{{Egress: "relay-b", Route: []string{"relay-a", "relay-b"}}},
	}}})

	candidates := resolver.resolveCandidates(config.ForwardConfig{Protocol: "tcp", Service: "echo", Egress: "relay-d", Route: []string{"relay-a", "relay-d"}})
	assertRouteCandidates(t, candidates, selectedRoute{service: "echo", egress: "relay-d", route: []string{"relay-a", "relay-d"}, e2eMode: config.ForwardE2EOff})
}

func TestRouteResolverExplicitEgressWinsOverStaticRoute(t *testing.T) {
	resolver := newRouteResolver(&config.Config{Routes: []config.ServiceRouteConfig{{
		Service:    "echo",
		Protocol:   "tcp",
		Candidates: []config.RouteCandidateConfig{{Egress: "relay-b", Route: []string{"relay-a", "relay-b"}}},
	}}})

	candidates := resolver.resolveCandidates(config.ForwardConfig{Protocol: "tcp", Service: "echo", Egress: "server-1"})
	assertRouteCandidates(t, candidates, selectedRoute{service: "echo", egress: "server-1", e2eMode: config.ForwardE2EOff})
}

func TestRouteResolverFallsBackToForwardWhenNoStaticRouteMatches(t *testing.T) {
	resolver := newRouteResolver(&config.Config{Routes: []config.ServiceRouteConfig{{
		Service:    "echo-b",
		Protocol:   "tcp",
		Candidates: []config.RouteCandidateConfig{{Egress: "relay-b", Route: []string{"relay-a", "relay-b"}}},
	}}})

	candidates := resolver.resolveCandidates(config.ForwardConfig{Protocol: "tcp", Service: "echo-a"})
	assertRouteCandidates(t, candidates, selectedRoute{service: "echo-a", e2eMode: config.ForwardE2EOff})
}

func TestSelectedRouteEffectiveE2ERequired(t *testing.T) {
	cases := []struct {
		name  string
		route selectedRoute
		want  bool
	}{
		{name: "off relayed", route: selectedRoute{e2eMode: config.ForwardE2EOff, route: []string{"relay-a", "relay-b"}}, want: false},
		{name: "auto direct no route", route: selectedRoute{e2eMode: config.ForwardE2EAuto}, want: false},
		{name: "auto direct one-hop route", route: selectedRoute{e2eMode: config.ForwardE2EAuto, route: []string{"server-1"}}, want: false},
		{name: "auto relayed", route: selectedRoute{e2eMode: config.ForwardE2EAuto, route: []string{"relay-a", "relay-b"}}, want: true},
		{name: "always direct", route: selectedRoute{e2eMode: config.ForwardE2EAlways}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.route.effectiveE2ERequired(); got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestIsRetryableSetupError(t *testing.T) {
	if !isRetryableSetupError(errors.New("transport failed")) {
		t.Fatal("expected transport errors to be retryable")
	}
	retryableCodes := []protocol.ConnectCode{
		protocol.ConnectCodeUnreachableEgress,
		protocol.ConnectCodeNextHopUnreachable,
		protocol.ConnectCodeTargetDialFailed,
	}
	for _, code := range retryableCodes {
		if !isRetryableSetupError(&ConnectRejectedError{Code: code}) {
			t.Fatalf("expected %s to be retryable", code)
		}
	}
	nonRetryableCodes := []protocol.ConnectCode{
		protocol.ConnectCodeAccessDenied,
		protocol.ConnectCodeServiceNotFound,
		protocol.ConnectCodeUnsupportedProtocol,
		protocol.ConnectCodeRouteInvalid,
		protocol.ConnectCodeInternalError,
	}
	for _, code := range nonRetryableCodes {
		if isRetryableSetupError(&ConnectRejectedError{Code: code}) {
			t.Fatalf("expected %s to be non-retryable", code)
		}
	}
}

func assertRouteCandidates(t *testing.T, candidates []selectedRoute, expected ...selectedRoute) {
	t.Helper()
	if !reflect.DeepEqual(candidates, expected) {
		t.Fatalf("unexpected route candidates: got %#v want %#v", candidates, expected)
	}
}
