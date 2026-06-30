package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/identity"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/quic-go/quic-go"
)

const (
	defaultShutdownWaitTimeout  = 5 * time.Second
	acceptFailureInitialBackoff = 100 * time.Millisecond
	acceptFailureMaxBackoff     = 30 * time.Second
)

type options struct {
	started        func(addr string)
	connectRequest func(req protocol.ConnectRequest)
}

type serverRuntime struct {
	ctx context.Context
	cfg *config.Config

	logger *slog.Logger
	opts   options
	peers  *peerSessions
	e2e    *e2eServerRuntime

	connWG sync.WaitGroup
}

type Option func(*options)

func WithStartedFunc(fn func(addr string)) Option {
	return func(opts *options) {
		opts.started = fn
	}
}

func WithConnectRequestFunc(fn func(req protocol.ConnectRequest)) Option {
	return func(opts *options) {
		opts.connectRequest = fn
	}
}

func Run(ctx context.Context, cfg *config.Config, logger *slog.Logger, runOptions ...Option) error {
	logger = ensureLogger(logger)
	if err := config.ValidateServer(cfg); err != nil {
		return err
	}

	tlsConfig, err := identity.ServerTLSConfig(cfg.Identity)
	if err != nil {
		return err
	}

	e2eRuntime, err := newE2EServerRuntime(cfg)
	if err != nil {
		return err
	}

	listener, err := quic.ListenAddr(cfg.Listen, tlsConfig, &quic.Config{})
	if err != nil {
		return err
	}
	defer listener.Close()

	opts := options{}
	for _, apply := range runOptions {
		apply(&opts)
	}

	addr := listener.Addr().String()
	logger.Info("server listening", "node_id", cfg.NodeID, "addr", addr)
	if opts.started != nil {
		opts.started(addr)
	}

	rt := &serverRuntime{ctx: ctx, cfg: cfg, logger: logger, opts: opts, e2e: e2eRuntime}
	rt.peers = newPeerSessions(cfg, logger)
	// Outbound peer connections are bidirectional, so they also need a stream accept loop.
	rt.peers.onConnected = rt.startConnection
	if err := rt.peers.ConnectAll(ctx); err != nil {
		rt.peers.Close("startup failed")
		return err
	}
	defer rt.peers.Close("server shutdown")
	acceptBackoff := acceptFailureInitialBackoff
	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				if waitErr := waitGroupTimeout(&rt.connWG, defaultShutdownWaitTimeout); waitErr != nil {
					return fmt.Errorf("server connection shutdown: %w", waitErr)
				}
				return nil
			}
			logger.Warn("accept connection failed", "backoff", acceptBackoff.String(), "error", err)
			select {
			case <-ctx.Done():
				if waitErr := waitGroupTimeout(&rt.connWG, defaultShutdownWaitTimeout); waitErr != nil {
					return fmt.Errorf("server connection shutdown: %w", waitErr)
				}
				return nil
			case <-time.After(acceptBackoff):
			}
			acceptBackoff *= 2
			if acceptBackoff > acceptFailureMaxBackoff {
				acceptBackoff = acceptFailureMaxBackoff
			}
			continue
		}
		acceptBackoff = acceptFailureInitialBackoff
		// Inbound listener connections use the same stream handling as peer-dialed connections.
		rt.startConnection(conn)
	}
}

func (rt *serverRuntime) startConnection(conn *quic.Conn) {
	rt.connWG.Go(func() {
		rt.handleConnection(conn)
	})
}

func waitGroupTimeout(wg *sync.WaitGroup, timeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timed out after %s", timeout)
	}
}
