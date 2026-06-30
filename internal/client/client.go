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
	"github.com/awithy/qoru/internal/protocol"
	"github.com/awithy/qoru/internal/requestid"
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
	logger = ensureLogger(logger)
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
	routes := newRouteResolver(cfg)
	e2eRuntime, err := newE2EClientRuntime(cfg)
	if err != nil {
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
			acceptForward(ctx, sessions, routes, e2eRuntime, item.forward, item.listener, logger, errCh, &handlerWG)
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
		logger.Info("client listening", "node_id", cfg.NodeID, "addr", addr, "service", item.forward.Service, "egress", item.forward.Egress)
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

func acceptForward(ctx context.Context, session upstreamSession, routes *routeResolver, e2eRuntime *e2eClientRuntime, forward config.ForwardConfig, listener net.Listener, logger *slog.Logger, errCh chan<- error, handlerWG *sync.WaitGroup) {
	for {
		localConn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			errCh <- err
			return
		}
		candidates := routes.resolveCandidates(forward)
		handlerWG.Add(1)
		go func() {
			defer handlerWG.Done()
			handleLocalConnection(ctx, session, e2eRuntime, candidates, localConn, logger)
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

func handleLocalConnection(ctx context.Context, session upstreamSession, e2eRuntime *e2eClientRuntime, candidates []selectedRoute, localConn net.Conn, logger *slog.Logger) {
	logger = ensureLogger(logger)
	defer localConn.Close()
	if len(candidates) == 0 {
		logger.Error("no route candidates available", "local_addr", localConn.LocalAddr().String(), "remote_addr", localConn.RemoteAddr().String())
		return
	}

	requestID, err := requestid.New()
	if err != nil {
		logger.Error("generate request id failed", "service", candidates[0].service, "error", err)
		return
	}
	logger = logger.With("request_id", requestID, "local_addr", localConn.LocalAddr().String(), "remote_addr", localConn.RemoteAddr().String(), "route_candidate_count", len(candidates))
	logger.Info("local tcp connection accepted")

	for i, candidate := range candidates {
		e2eRequired := candidate.effectiveE2ERequired()
		candidateLogger := logger.With("service", candidate.service, "egress", candidate.egress, "route", candidate.route, "route_candidate", i+1, "e2e", candidate.e2eMode, "e2e_required", e2eRequired)
		stream, err := session.OpenTCPStream(ctx, requestID, candidate.service, candidate.egress, candidate.route, e2eRequired)
		if err == nil {
			candidateLogger.Info("tcp stream connected")
			if e2eRequired {
				reader, writer, handshakeErr := e2eRuntime.runHandshake(stream, requestID, candidate, candidateLogger)
				if handshakeErr != nil {
					candidateLogger.Error("e2e handshake failed", "error", handshakeErr)
					_ = stream.Close()
					return
				}
				proxyEncryptedTCP(localConn, stream, reader, writer)
			} else {
				proxyTCP(localConn, stream)
			}
			candidateLogger.Info("local tcp proxy closed")
			return
		}

		retry := ctx.Err() == nil && i+1 < len(candidates) && isRetryableSetupError(err)
		logOpenTCPStreamFailed(candidateLogger, err, retry)
		if !retry {
			return
		}
	}
}

func logOpenTCPStreamFailed(logger *slog.Logger, err error, retryNextCandidate bool) {
	var rejected *ConnectRejectedError
	var backoff *ReconnectBackoffError
	switch {
	case errors.As(err, &rejected):
		logger.Warn("tcp service rejected", "response_code", rejected.Code.String(), "retry_next_candidate", retryNextCandidate, "error", err)
	case errors.As(err, &backoff):
		logger.Warn(
			"upstream reconnect backoff active",
			"server_id", backoff.ServerID,
			"addr", backoff.Address,
			"next_attempt", backoff.NextAttempt.Format(time.RFC3339Nano),
			"retry_next_candidate", retryNextCandidate,
			"error", err,
		)
	default:
		logger.Error("open tcp stream failed", "retry_next_candidate", retryNextCandidate, "error", err)
	}
}

func isRetryableSetupError(err error) bool {
	var rejected *ConnectRejectedError
	if !errors.As(err, &rejected) {
		return true
	}
	switch rejected.Code {
	case protocol.ConnectCodeUnreachableEgress, protocol.ConnectCodeNextHopUnreachable, protocol.ConnectCodeTargetDialFailed:
		return true
	default:
		return false
	}
}
