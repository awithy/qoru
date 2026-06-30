package client

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/quic-go/quic-go"
)

var reconnectBackoffAfterFailure = []time.Duration{
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
}

type upstreamDialer func(context.Context, string, config.IdentityConfig, config.ServerConfig, *slog.Logger) (*quic.Conn, error)

type upstreamSession interface {
	OpenTCPStream(ctx context.Context, requestID, service, egress string, route []string, e2eRequired bool) (*quic.Stream, error)
	Close(reason string)
}

type upstreamSessions struct {
	sessions  map[string]*reconnectingUpstreamSession
	defaultID string
}

func newUpstreamSessions(cfg *config.Config, logger *slog.Logger) (*upstreamSessions, error) {
	servers, err := configuredServers(cfg)
	if err != nil {
		return nil, err
	}

	sessions := make(map[string]*reconnectingUpstreamSession, len(servers))
	var defaultID string
	for _, server := range servers {
		sessions[server.ID] = newReconnectingUpstreamSession(cfg.NodeID, cfg.Identity, server, logger)
		defaultID = server.ID
	}
	if len(sessions) != 1 {
		defaultID = ""
	}

	return &upstreamSessions{sessions: sessions, defaultID: defaultID}, nil
}

func configuredServers(cfg *config.Config) ([]config.ServerConfig, error) {
	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("at least one servers entry is required for client mode")
	}
	servers := make([]config.ServerConfig, len(cfg.Servers))
	copy(servers, cfg.Servers)
	return servers, nil
}

func (s *upstreamSessions) ConnectAll(ctx context.Context) error {
	for id, session := range s.sessions {
		if _, err := session.connection(ctx); err != nil {
			return fmt.Errorf("connect upstream %q: %w", id, err)
		}
	}
	return nil
}

func (s *upstreamSessions) OpenTCPStream(ctx context.Context, requestID, service, egress string, route []string, e2eRequired bool) (*quic.Stream, error) {
	selector := egress
	if len(route) > 0 {
		selector = route[0]
	}
	session, selectedEgress, err := s.selectSession(selector)
	if err != nil {
		return nil, err
	}
	if len(route) > 0 {
		selectedEgress = egress
	}
	return session.OpenTCPStream(ctx, requestID, service, selectedEgress, route, e2eRequired)
}

func (s *upstreamSessions) Close(reason string) {
	for _, session := range s.sessions {
		session.Close(reason)
	}
}

func (s *upstreamSessions) selectSession(egress string) (*reconnectingUpstreamSession, string, error) {
	if egress == "" {
		if s.defaultID == "" {
			return nil, "", fmt.Errorf("egress is required when multiple servers are configured")
		}
		return s.sessions[s.defaultID], "", nil
	}

	session, ok := s.sessions[egress]
	if !ok {
		return nil, "", fmt.Errorf("egress %q does not match a configured server", egress)
	}
	return session, egress, nil
}

type reconnectingUpstreamSession struct {
	nodeID   string
	identity config.IdentityConfig
	server   config.ServerConfig
	logger   *slog.Logger

	mu     sync.Mutex
	conn   *quic.Conn
	closed bool

	dial             upstreamDialer
	now              func() time.Time
	nextDial         time.Time
	lastDialErr      error
	backoffFailCount int
}

func newReconnectingUpstreamSession(nodeID string, identity config.IdentityConfig, server config.ServerConfig, logger *slog.Logger) *reconnectingUpstreamSession {
	logger = ensureLogger(logger)
	return &reconnectingUpstreamSession{
		nodeID:   nodeID,
		identity: identity,
		server:   server,
		logger:   logger,
		dial:     ConnectToServer,
		now:      time.Now,
	}
}

func (s *reconnectingUpstreamSession) OpenTCPStream(ctx context.Context, requestID, service, egress string, route []string, e2eRequired bool) (*quic.Stream, error) {
	conn, err := s.connection(ctx)
	if err != nil {
		return nil, err
	}

	stream, err := OpenTCPStream(ctx, conn, requestID, service, egress, route, e2eRequired)
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
	return OpenTCPStream(ctx, conn, requestID, service, egress, route, e2eRequired)
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

	now := s.now()
	if !s.nextDial.IsZero() && now.Before(s.nextDial) {
		return nil, &ReconnectBackoffError{
			ServerID:    s.server.ID,
			Address:     s.server.Address,
			NextAttempt: s.nextDial,
			Err:         s.lastDialErr,
		}
	}

	hadFailures := s.backoffFailCount > 0
	if hadFailures {
		s.logger.Info("upstream reconnecting", "server_id", s.server.ID, "addr", s.server.Address)
	}

	conn, err := s.dial(ctx, s.nodeID, s.identity, s.server, s.logger)
	if err != nil {
		s.recordDialFailure(now, err)
		s.logger.Warn(
			"upstream reconnect failed",
			"server_id", s.server.ID,
			"addr", s.server.Address,
			"backoff", s.nextDial.Sub(now).String(),
			"next_attempt", s.nextDial.Format(time.RFC3339Nano),
			"error", err,
		)
		return nil, err
	}
	s.conn = conn
	s.resetDialBackoff()
	if hadFailures {
		s.logger.Info("upstream reconnect succeeded", "server_id", s.server.ID, "addr", s.server.Address)
	}
	return conn, nil
}

func (s *reconnectingUpstreamSession) recordDialFailure(now time.Time, err error) {
	delay := reconnectBackoffAfterFailure[len(reconnectBackoffAfterFailure)-1]
	if s.backoffFailCount < len(reconnectBackoffAfterFailure) {
		delay = reconnectBackoffAfterFailure[s.backoffFailCount]
	}
	s.backoffFailCount++
	s.nextDial = now.Add(delay)
	s.lastDialErr = err
}

func (s *reconnectingUpstreamSession) resetDialBackoff() {
	s.nextDial = time.Time{}
	s.lastDialErr = nil
	s.backoffFailCount = 0
}

func (s *reconnectingUpstreamSession) dropConnection(conn *quic.Conn, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == conn {
		s.logger.Warn("upstream connection dropped", "server_id", s.server.ID, "addr", s.server.Address, "reason", reason)
		_ = s.conn.CloseWithError(0, reason)
		s.conn = nil
	}
}
