package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/identity"
	"github.com/quic-go/quic-go"
)

const defaultPeerDialTimeout = 10 * time.Second

type peerSessions struct {
	nodeID      string
	identity    config.IdentityConfig
	peers       map[string]config.PeerConfig
	logger      *slog.Logger
	onConnected func(*quic.Conn)

	mu    sync.Mutex
	conns map[string]*quic.Conn
}

func newPeerSessions(cfg *config.Config, logger *slog.Logger) *peerSessions {
	peers := make(map[string]config.PeerConfig, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		peers[peer.ID] = peer
	}
	return &peerSessions{nodeID: cfg.NodeID, identity: cfg.Identity, peers: peers, logger: logger, conns: make(map[string]*quic.Conn)}
}

func (s *peerSessions) ConnectAll(ctx context.Context) error {
	for _, peer := range s.peers {
		if !peer.Dial {
			continue
		}
		if _, err := s.connection(ctx, peer.ID); err != nil {
			return fmt.Errorf("connect peer %q: %w", peer.ID, err)
		}
	}
	return nil
}

func (s *peerSessions) RegisterInbound(peerID string, conn *quic.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.peers[peerID]; !ok {
		return false
	}
	if existing := s.conns[peerID]; existing == conn && existing.Context().Err() == nil {
		return true
	}
	if existing := s.conns[peerID]; existing != nil && existing.Context().Err() == nil {
		s.logger.Warn("duplicate peer session rejected", "peer_id", peerID, "direction", "inbound")
		_ = conn.CloseWithError(0, "duplicate peer session unsupported")
		return true
	}
	s.conns[peerID] = conn
	s.logger.Info("peer connected", "peer_id", peerID, "direction", "inbound")
	return true
}

func (s *peerSessions) OpenStream(ctx context.Context, peerID string) (*quic.Stream, error) {
	conn, err := s.connection(ctx, peerID)
	if err != nil {
		return nil, err
	}
	stream, err := conn.OpenStreamSync(ctx)
	if err == nil {
		return stream, nil
	}
	s.drop(peerID, conn, "open stream failed")
	conn, connErr := s.connection(ctx, peerID)
	if connErr != nil {
		return nil, fmt.Errorf("%w; reconnect failed: %v", err, connErr)
	}
	return conn.OpenStreamSync(ctx)
}

func (s *peerSessions) Close(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, conn := range s.conns {
		_ = conn.CloseWithError(0, reason)
		delete(s.conns, id)
	}
}

func (s *peerSessions) connection(ctx context.Context, peerID string) (*quic.Conn, error) {
	s.mu.Lock()
	conn := s.conns[peerID]
	if conn != nil && conn.Context().Err() == nil {
		s.mu.Unlock()
		return conn, nil
	}
	if conn != nil {
		_ = conn.CloseWithError(0, "reconnecting")
		delete(s.conns, peerID)
	}
	peer, ok := s.peers[peerID]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("peer %q is not configured", peerID)
	}
	if peer.Address == "" {
		return nil, fmt.Errorf("peer %q has no dial address", peerID)
	}

	tlsConfig, err := identity.ClientTLSConfig(s.identity, peerID)
	if err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, defaultPeerDialTimeout)
	defer cancel()
	dialed, err := quic.DialAddr(dialCtx, peer.Address, tlsConfig, &quic.Config{})
	if err != nil {
		return nil, err
	}
	s.logger.Info("peer connected", "peer_id", peerID, "addr", peer.Address, "direction", "outbound")

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing := s.conns[peerID]; existing != nil && existing.Context().Err() == nil {
		s.logger.Warn("duplicate peer session rejected", "peer_id", peerID, "direction", "outbound")
		_ = dialed.CloseWithError(0, "duplicate peer session unsupported")
		return existing, nil
	}
	s.conns[peerID] = dialed
	if s.onConnected != nil {
		s.onConnected(dialed)
	}
	return dialed, nil
}

func (s *peerSessions) drop(peerID string, conn *quic.Conn, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[peerID] == conn {
		_ = conn.CloseWithError(0, reason)
		delete(s.conns, peerID)
	}
}
