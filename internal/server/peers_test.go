package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/awithy/qoru/internal/config"
)

func TestPeerConnectionBackoffFailsFast(t *testing.T) {
	cfg := &config.Config{
		NodeID:   "relay-a",
		Mode:     config.ModeServer,
		Identity: config.IdentityConfig{Cert: "../../dev/certs/relay-a.crt", Key: "../../dev/certs/relay-a.key", CA: "../../dev/certs/ca.crt"},
		Peers:    []config.PeerConfig{{ID: "relay-b", Address: "127.0.0.1:1", Dial: true}},
	}
	sessions := newPeerSessions(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Unix(1000, 0)
	sessions.now = func() time.Time { return now }

	if _, err := sessions.connection(context.Background(), "relay-b"); err == nil {
		t.Fatal("expected initial dial to fail")
	}

	_, err := sessions.connection(context.Background(), "relay-b")
	var backoff *PeerReconnectBackoffError
	if !errors.As(err, &backoff) {
		t.Fatalf("expected PeerReconnectBackoffError, got %T: %v", err, err)
	}
	if backoff.NextAttempt != now.Add(500*time.Millisecond) {
		t.Fatalf("expected first backoff next attempt %s, got %s", now.Add(500*time.Millisecond), backoff.NextAttempt)
	}
}

func TestPeerBackoffCapsAtMaxInterval(t *testing.T) {
	cfg := &config.Config{NodeID: "relay-a", Peers: []config.PeerConfig{{ID: "relay-b", Address: "127.0.0.1:1", Dial: true}}}
	sessions := newPeerSessions(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Unix(1000, 0)
	sessions.now = func() time.Time { return now }
	peer := cfg.Peers[0]

	for i := 0; i < len(peerReconnectBackoffAfterFailure)+3; i++ {
		sessions.recordDialFailure("relay-b", peer, context.DeadlineExceeded)
	}

	max := peerReconnectBackoffAfterFailure[len(peerReconnectBackoffAfterFailure)-1]
	if got := sessions.reconnect["relay-b"].nextDial.Sub(now); got != max {
		t.Fatalf("expected capped backoff %s, got %s", max, got)
	}
}

func TestPeerReconnectStateIsPerPeer(t *testing.T) {
	cfg := &config.Config{NodeID: "relay-a", Peers: []config.PeerConfig{
		{ID: "relay-b", Address: "127.0.0.1:1", Dial: true},
		{ID: "relay-c", Address: "127.0.0.1:2", Dial: true},
	}}
	sessions := newPeerSessions(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Unix(1000, 0)
	sessions.now = func() time.Time { return now }

	sessions.recordDialFailure("relay-b", cfg.Peers[0], context.DeadlineExceeded)

	if _, ok := sessions.reconnect["relay-b"]; !ok {
		t.Fatal("expected relay-b reconnect state")
	}
	if _, ok := sessions.reconnect["relay-c"]; ok {
		t.Fatal("did not expect relay-c reconnect state")
	}

	sessions.recordDialFailure("relay-c", cfg.Peers[1], context.Canceled)
	if sessions.reconnect["relay-b"].lastDialErr != context.DeadlineExceeded {
		t.Fatal("relay-c failure changed relay-b reconnect state")
	}
	if sessions.reconnect["relay-c"].lastDialErr != context.Canceled {
		t.Fatal("relay-c reconnect state was not recorded")
	}

	sessions.resetDialBackoff("relay-b")
	if _, ok := sessions.reconnect["relay-b"]; ok {
		t.Fatal("expected relay-b reconnect state to be reset")
	}
	if _, ok := sessions.reconnect["relay-c"]; !ok {
		t.Fatal("relay-b reset should not clear relay-c reconnect state")
	}
}
