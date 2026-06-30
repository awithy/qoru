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

var peerReconnectBackoffAfterFailure = []time.Duration{
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
}

type PeerReconnectBackoffError struct {
	PeerID      string
	Address     string
	NextAttempt time.Time
	Err         error
}

func (e *PeerReconnectBackoffError) Error() string {
	msg := "peer reconnect backoff active until " + e.NextAttempt.Format(time.RFC3339Nano)
	if e.Err != nil {
		return msg + ": " + e.Err.Error()
	}
	return msg
}

func (e *PeerReconnectBackoffError) Unwrap() error {
	return e.Err
}

type peerReconnectState struct {
	nextDial    time.Time
	lastDialErr error
	failCount   int
}

type peerSessions struct {
	nodeID      string
	identity    config.IdentityConfig
	peers       map[string]config.PeerConfig
	logger      *slog.Logger
	onConnected func(*quic.Conn)

	mu        sync.Mutex
	conns     map[string]*quic.Conn
	reconnect map[string]*peerReconnectState
	now       func() time.Time
}

func newPeerSessions(cfg *config.Config, logger *slog.Logger) *peerSessions {
	peers := make(map[string]config.PeerConfig, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		peers[peer.ID] = peer
	}
	return &peerSessions{
		nodeID:    cfg.NodeID,
		identity:  cfg.Identity,
		peers:     peers,
		logger:    logger,
		conns:     make(map[string]*quic.Conn),
		reconnect: make(map[string]*peerReconnectState),
		now:       time.Now,
	}
}

func (s *peerSessions) ConnectAll(ctx context.Context) error {
	for _, peer := range s.peers {
		if !peer.Dial {
			continue
		}
		if _, err := s.connection(ctx, peer.ID); err != nil && s.logger != nil {
			s.logger.Warn("peer startup connect failed", "peer_id", peer.ID, "addr", peer.Address, "error", err)
		}
		go s.maintainConnection(ctx, peer.ID)
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
	s.resetDialBackoff(peerID)
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
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("peer %q is not configured", peerID)
	}
	if peer.Address == "" {
		s.mu.Unlock()
		return nil, fmt.Errorf("peer %q has no dial address", peerID)
	}
	now := s.now()
	state := s.reconnect[peerID]
	if state != nil && !state.nextDial.IsZero() && now.Before(state.nextDial) {
		err := &PeerReconnectBackoffError{PeerID: peerID, Address: peer.Address, NextAttempt: state.nextDial, Err: state.lastDialErr}
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()

	tlsConfig, err := identity.ClientTLSConfig(s.identity, peerID)
	if err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, defaultPeerDialTimeout)
	defer cancel()
	dialed, err := quic.DialAddr(dialCtx, peer.Address, tlsConfig, &quic.Config{})
	if err != nil {
		s.recordDialFailure(peerID, peer, err)
		return nil, err
	}
	if s.logger != nil {
		s.logger.Info("peer connected", "peer_id", peerID, "addr", peer.Address, "direction", "outbound")
	}

	s.mu.Lock()
	if existing := s.conns[peerID]; existing != nil && existing.Context().Err() == nil {
		if s.logger != nil {
			s.logger.Warn("duplicate peer session rejected", "peer_id", peerID, "direction", "outbound")
		}
		_ = dialed.CloseWithError(0, "duplicate peer session unsupported")
		s.mu.Unlock()
		return existing, nil
	}
	s.conns[peerID] = dialed
	s.resetDialBackoff(peerID)
	onConnected := s.onConnected
	s.mu.Unlock()
	if onConnected != nil {
		onConnected(dialed)
	}
	return dialed, nil
}

func (s *peerSessions) maintainConnection(ctx context.Context, peerID string) {
	for ctx.Err() == nil {
		conn, err := s.connection(ctx, peerID)
		if err != nil {
			if s.logger != nil {
				attrs := []any{"peer_id", peerID, "error", err}
				if backoff, ok := err.(*PeerReconnectBackoffError); ok {
					attrs = append(attrs, "addr", backoff.Address, "next_attempt", backoff.NextAttempt.Format(time.RFC3339Nano))
				}
				s.logger.Warn("peer reconnect failed", attrs...)
			}
			s.sleepUntilNextDial(ctx, peerID)
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-conn.Context().Done():
			s.drop(peerID, conn, "peer disconnected")
		}
	}
}

func (s *peerSessions) sleepUntilNextDial(ctx context.Context, peerID string) {
	s.mu.Lock()
	state := s.reconnect[peerID]
	var next time.Time
	if state != nil {
		next = state.nextDial
	}
	now := s.now()
	s.mu.Unlock()

	delay := 500 * time.Millisecond
	if !next.IsZero() && next.After(now) {
		delay = next.Sub(now)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (s *peerSessions) recordDialFailure(peerID string, peer config.PeerConfig, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	state := s.reconnect[peerID]
	if state == nil {
		state = &peerReconnectState{}
		s.reconnect[peerID] = state
	}
	delay := peerReconnectBackoffAfterFailure[len(peerReconnectBackoffAfterFailure)-1]
	if state.failCount < len(peerReconnectBackoffAfterFailure) {
		delay = peerReconnectBackoffAfterFailure[state.failCount]
	}
	state.failCount++
	state.nextDial = now.Add(delay)
	state.lastDialErr = err
	if s.logger != nil {
		s.logger.Warn("peer reconnect scheduled", "peer_id", peerID, "addr", peer.Address, "backoff", delay.String(), "next_attempt", state.nextDial.Format(time.RFC3339Nano), "error", err)
	}
}

func (s *peerSessions) resetDialBackoff(peerID string) {
	delete(s.reconnect, peerID)
}

func (s *peerSessions) drop(peerID string, conn *quic.Conn, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[peerID] == conn {
		_ = conn.CloseWithError(0, reason)
		delete(s.conns, peerID)
	}
}
