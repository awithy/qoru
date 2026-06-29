package client

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/quic-go/quic-go"
)

func TestReconnectingUpstreamSessionBackoffFailsFastWithoutSleeping(t *testing.T) {
	fakeNow := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	attempts := 0
	dialErr := errors.New("dial failed")
	session := newTestReconnectingUpstreamSession()
	session.now = func() time.Time { return fakeNow }
	session.dial = func(context.Context, string, config.IdentityConfig, config.ServerConfig, *slog.Logger) (*quic.Conn, error) {
		attempts++
		return nil, dialErr
	}

	if _, err := session.connection(context.Background()); err == nil {
		t.Fatal("expected first dial to fail")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 dial attempt, got %d", attempts)
	}
	if want := fakeNow.Add(500 * time.Millisecond); !session.nextDial.Equal(want) {
		t.Fatalf("expected next dial %s, got %s", want, session.nextDial)
	}

	if _, err := session.connection(context.Background()); err == nil {
		t.Fatal("expected backoff error")
	}
	if attempts != 1 {
		t.Fatalf("expected no dial attempt during backoff, got %d", attempts)
	}

	fakeNow = session.nextDial
	if _, err := session.connection(context.Background()); err == nil {
		t.Fatal("expected second dial to fail")
	}
	if attempts != 2 {
		t.Fatalf("expected second dial attempt after backoff, got %d", attempts)
	}
	if want := fakeNow.Add(1 * time.Second); !session.nextDial.Equal(want) {
		t.Fatalf("expected next dial %s, got %s", want, session.nextDial)
	}
}

func TestReconnectingUpstreamSessionBackoffCapsAndResetsAfterSuccess(t *testing.T) {
	fakeNow := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	session := newTestReconnectingUpstreamSession()
	session.now = func() time.Time { return fakeNow }

	for i, want := range []time.Duration{
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		16 * time.Second,
		16 * time.Second,
	} {
		err := errors.New("dial failed")
		session.recordDialFailure(fakeNow, err)
		if got := session.nextDial.Sub(fakeNow); got != want {
			t.Fatalf("failure %d: expected backoff %s, got %s", i+1, want, got)
		}
	}

	session.resetDialBackoff()
	if !session.nextDial.IsZero() {
		t.Fatalf("expected nextDial reset, got %s", session.nextDial)
	}
	if session.lastDialErr != nil {
		t.Fatalf("expected lastDialErr reset, got %v", session.lastDialErr)
	}
	if session.backoffFailCount != 0 {
		t.Fatalf("expected fail count reset, got %d", session.backoffFailCount)
	}
}

func newTestReconnectingUpstreamSession() *reconnectingUpstreamSession {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &config.ServerConfig{ID: "server-1", Address: "127.0.0.1:4433"}
	return newReconnectingUpstreamSession("client-1", config.IdentityConfig{}, server, logger)
}
