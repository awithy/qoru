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

const defaultShutdownWaitTimeout = 5 * time.Second

type options struct {
	started        func(addr string)
	connectRequest func(req protocol.ConnectRequest)
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
	if err := config.ValidateServer(cfg); err != nil {
		return err
	}

	tlsConfig, err := identity.ServerTLSConfig(cfg.Identity)
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
	if logger != nil {
		logger.Info("server listening", "node_id", cfg.NodeID, "addr", addr)
	}
	if opts.started != nil {
		opts.started(addr)
	}

	var connWG sync.WaitGroup
	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				if waitErr := waitGroupTimeout(&connWG, defaultShutdownWaitTimeout); waitErr != nil {
					return fmt.Errorf("server connection shutdown: %w", waitErr)
				}
				return nil
			}
			return err
		}
		connWG.Add(1)
		go func() {
			defer connWG.Done()
			handleConnection(ctx, cfg, conn, logger, opts)
		}()
	}
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
