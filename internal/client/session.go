package client

import (
	"context"
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

func configuredServers(cfg *config.Config) ([]*config.ServerConfig, error) {
	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("at least one servers entry is required for client mode")
	}
	servers := make([]*config.ServerConfig, 0, len(cfg.Servers))
	for i := range cfg.Servers {
		servers = append(servers, &cfg.Servers[i])
	}
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

func (s *upstreamSessions) OpenTCPStream(ctx context.Context, service, egress string) (*quic.Stream, error) {
	session, selectedEgress, err := s.selectSession(egress)
	if err != nil {
		return nil, err
	}
	return session.OpenTCPStream(ctx, service, selectedEgress)
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
	server   *config.ServerConfig
	logger   *slog.Logger

	mu     sync.Mutex
	conn   *quic.Conn
	closed bool
}

func newReconnectingUpstreamSession(nodeID string, identity config.IdentityConfig, server *config.ServerConfig, logger *slog.Logger) *reconnectingUpstreamSession {
	return &reconnectingUpstreamSession{nodeID: nodeID, identity: identity, server: server, logger: logger}
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

	conn, err := ConnectToServer(ctx, s.nodeID, s.identity, *s.server, s.logger)
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
