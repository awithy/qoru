package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/awithy/qoru/internal/config"
)

const defaultShutdownWaitTimeout = 5 * time.Second

type options struct {
	started func(addr string)
}

type Option func(*options)

func WithStartedFunc(fn func(addr string)) Option {
	return func(opts *options) {
		opts.started = fn
	}
}

type forwardListener struct {
	forward  config.ForwardConfig
	listener net.Listener
}

func Run(ctx context.Context, cfg *config.Config, logger *slog.Logger, runOptions ...Option) error {
	if err := config.ValidateClient(cfg); err != nil {
		return err
	}

	opts := options{}
	for _, apply := range runOptions {
		apply(&opts)
	}

	sessions, err := newUpstreamSessions(cfg, logger)
	if err != nil {
		return err
	}
	if err := sessions.ConnectAll(ctx); err != nil {
		return err
	}

	listeners := make([]forwardListener, 0, len(cfg.Forwards))
	for _, forward := range cfg.Forwards {
		listener, err := net.Listen("tcp", forward.Listen)
		if err != nil {
			closeListeners(listeners)
			return err
		}
		listeners = append(listeners, forwardListener{forward: forward, listener: listener})
	}

	var acceptWG sync.WaitGroup
	var handlerWG sync.WaitGroup
	errCh := make(chan error, len(listeners))
	for _, item := range listeners {
		acceptWG.Add(1)
		go func(item forwardListener) {
			defer acceptWG.Done()
			acceptForward(ctx, sessions, item.forward, item.listener, logger, errCh, &handlerWG)
		}(item)
	}

	shutdown := func(reason string) error {
		closeListeners(listeners)
		sessions.Close(reason)
		if err := waitGroupTimeout(&acceptWG, defaultShutdownWaitTimeout); err != nil {
			return fmt.Errorf("client accept shutdown: %w", err)
		}
		if err := waitGroupTimeout(&handlerWG, defaultShutdownWaitTimeout); err != nil {
			return fmt.Errorf("client connection shutdown: %w", err)
		}
		return nil
	}

	for _, item := range listeners {
		addr := item.listener.Addr().String()
		if logger != nil {
			logger.Info("client listening", "node_id", cfg.NodeID, "addr", addr, "service", item.forward.Service, "egress", item.forward.Egress)
		}
		if opts.started != nil {
			opts.started(addr)
		}
	}

	select {
	case <-ctx.Done():
		return shutdown("context canceled")
	case err := <-errCh:
		if ctx.Err() != nil {
			return shutdown("context canceled")
		}
		if shutdownErr := shutdown("listener error"); shutdownErr != nil {
			return fmt.Errorf("%w; %v", err, shutdownErr)
		}
		return err
	}
}

func closeListeners(listeners []forwardListener) {
	for _, item := range listeners {
		_ = item.listener.Close()
	}
}

func acceptForward(ctx context.Context, session upstreamSession, forward config.ForwardConfig, listener net.Listener, logger *slog.Logger, errCh chan<- error, handlerWG *sync.WaitGroup) {
	for {
		localConn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			errCh <- err
			return
		}
		handlerWG.Add(1)
		go func() {
			defer handlerWG.Done()
			handleLocalConnection(ctx, session, forward.Service, forward.Egress, localConn, logger)
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

func handleLocalConnection(ctx context.Context, session upstreamSession, service, egress string, localConn net.Conn, logger *slog.Logger) {
	defer localConn.Close()

	stream, err := session.OpenTCPStream(ctx, service, egress)
	if err != nil {
		if logger != nil {
			var rejected *ConnectRejectedError
			var backoff *ReconnectBackoffError
			switch {
			case errors.As(err, &rejected):
				logger.Warn("tcp service rejected", "service", service, "egress", egress, "error", err)
			case errors.As(err, &backoff):
				logger.Warn(
					"upstream reconnect backoff active",
					"service", service,
					"egress", egress,
					"server_id", backoff.ServerID,
					"addr", backoff.Address,
					"next_attempt", backoff.NextAttempt.Format(time.RFC3339Nano),
					"error", err,
				)
			default:
				logger.Error("open tcp stream failed", "service", service, "egress", egress, "error", err)
			}
		}
		return
	}

	proxyTCP(localConn, stream)
}
