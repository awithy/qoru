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
		selectedRoute{service: "echo", egress: "relay-b", route: []string{"relay-a", "relay-b"}},
		selectedRoute{service: "echo", egress: "relay-c", route: []string{"relay-a", "relay-c"}},
	)
}

func TestRouteResolverExplicitRouteWinsOverStaticRoute(t *testing.T) {
	resolver := newRouteResolver(&config.Config{Routes: []config.ServiceRouteConfig{{
		Service:    "echo",
		Protocol:   "tcp",
		Candidates: []config.RouteCandidateConfig{{Egress: "relay-b", Route: []string{"relay-a", "relay-b"}}},
	}}})

	candidates := resolver.resolveCandidates(config.ForwardConfig{Protocol: "tcp", Service: "echo", Egress: "relay-d", Route: []string{"relay-a", "relay-d"}})
	assertRouteCandidates(t, candidates, selectedRoute{service: "echo", egress: "relay-d", route: []string{"relay-a", "relay-d"}})
}

func TestRouteResolverExplicitEgressWinsOverStaticRoute(t *testing.T) {
	resolver := newRouteResolver(&config.Config{Routes: []config.ServiceRouteConfig{{
		Service:    "echo",
		Protocol:   "tcp",
		Candidates: []config.RouteCandidateConfig{{Egress: "relay-b", Route: []string{"relay-a", "relay-b"}}},
	}}})

	candidates := resolver.resolveCandidates(config.ForwardConfig{Protocol: "tcp", Service: "echo", Egress: "server-1"})
	assertRouteCandidates(t, candidates, selectedRoute{service: "echo", egress: "server-1"})
}

func TestRouteResolverFallsBackToForwardWhenNoStaticRouteMatches(t *testing.T) {
	resolver := newRouteResolver(&config.Config{Routes: []config.ServiceRouteConfig{{
		Service:    "echo-b",
		Protocol:   "tcp",
		Candidates: []config.RouteCandidateConfig{{Egress: "relay-b", Route: []string{"relay-a", "relay-b"}}},
	}}})

	candidates := resolver.resolveCandidates(config.ForwardConfig{Protocol: "tcp", Service: "echo-a"})
	assertRouteCandidates(t, candidates, selectedRoute{service: "echo-a"})
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
