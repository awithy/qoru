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
	if got := sessions.nextDial["relay-b"].Sub(now); got != max {
		t.Fatalf("expected capped backoff %s, got %s", max, got)
	}
}
