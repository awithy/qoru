package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/awithy/qoru/internal/config"
	"github.com/quic-go/quic-go"
)

type upstreamSession interface {
	OpenTCPStream(ctx context.Context, service, egress string) (*quic.Stream, error)
	Close(reason string)
}

type reconnectingUpstreamSession struct {
	cfg    *config.Config
	logger *slog.Logger

	mu     sync.Mutex
	conn   *quic.Conn
	closed bool
}

func newReconnectingUpstreamSession(cfg *config.Config, logger *slog.Logger) *reconnectingUpstreamSession {
	return &reconnectingUpstreamSession{cfg: cfg, logger: logger}
}

func (s *reconnectingUpstreamSession) OpenTCPStream(ctx context.Context, service, egress string) (*quic.Stream, error) {
	conn, err := s.connection(ctx)
	if err != nil {
		return nil, err
	}

	stream, err := OpenTCPStream(ctx, conn, service, egress)
	if err == nil {
		return stream, nil
	}
	if ctx.Err() != nil || isConnectRejected(err) {
		return nil, err
	}

	s.dropConnection(conn, "open tcp stream failed")
	conn, connErr := s.connection(ctx)
	if connErr != nil {
		return nil, fmt.Errorf("%w; reconnect failed: %v", err, connErr)
	}
	return OpenTCPStream(ctx, conn, service, egress)
}

func (s *reconnectingUpstreamSession) Close(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.conn != nil {
		_ = s.conn.CloseWithError(0, reason)
		s.conn = nil
	}
}

func (s *reconnectingUpstreamSession) connection(ctx context.Context) (*quic.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, fmt.Errorf("client session closed")
	}
	if s.conn != nil && s.conn.Context().Err() == nil {
		return s.conn, nil
	}
	if s.conn != nil {
		_ = s.conn.CloseWithError(0, "reconnecting")
		s.conn = nil
	}

	conn, err := Connect(ctx, s.cfg, s.logger)
	if err != nil {
		return nil, err
	}
	s.conn = conn
	return conn, nil
}

func (s *reconnectingUpstreamSession) dropConnection(conn *quic.Conn, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == conn {
		_ = s.conn.CloseWithError(0, reason)
		s.conn = nil
	}
}

func isConnectRejected(err error) bool {
	var rejected *ConnectRejectedError
	return errors.As(err, &rejected)
}
