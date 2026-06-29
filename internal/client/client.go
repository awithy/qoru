package client

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/identity"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/quic-go/quic-go"
)

const defaultQUICDialTimeout = 10 * time.Second

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
	forward  config.TCPForwardConfig
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

	conn, err := Connect(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer conn.CloseWithError(0, "done")

	listeners := make([]forwardListener, 0, len(cfg.TCPForwards))
	for _, forward := range cfg.TCPForwards {
		listener, err := net.Listen("tcp", forward.Listen)
		if err != nil {
			closeListeners(listeners)
			return err
		}
		listeners = append(listeners, forwardListener{forward: forward, listener: listener})
	}
	defer closeListeners(listeners)

	go func() {
		<-ctx.Done()
		closeListeners(listeners)
	}()

	errCh := make(chan error, len(listeners))
	for _, item := range listeners {
		go acceptForward(ctx, conn, item.forward, item.listener, logger, errCh)
	}

	for _, item := range listeners {
		addr := item.listener.Addr().String()
		if logger != nil {
			logger.Info("client listening", "node_id", cfg.NodeID, "addr", addr, "target", item.forward.Target)
		}
		if opts.started != nil {
			opts.started(addr)
		}
	}

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
}

func closeListeners(listeners []forwardListener) {
	for _, item := range listeners {
		_ = item.listener.Close()
	}
}

func acceptForward(ctx context.Context, conn *quic.Conn, forward config.TCPForwardConfig, listener net.Listener, logger *slog.Logger, errCh chan<- error) {
	for {
		localConn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			errCh <- err
			return
		}
		go handleLocalConnection(ctx, conn, forward.Target, localConn, logger)
	}
}

func handleLocalConnection(ctx context.Context, conn *quic.Conn, target string, localConn net.Conn, logger *slog.Logger) {
	defer localConn.Close()

	stream, err := OpenTCPStream(ctx, conn, target)
	if err != nil {
		if logger != nil {
			logger.Error("open tcp stream failed", "target", target, "error", err)
		}
		return
	}

	proxyTCP(localConn, stream)
}

func Connect(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*quic.Conn, error) {
	if err := config.ValidateClient(cfg); err != nil {
		return nil, err
	}

	tlsConfig, err := identity.ClientTLSConfig(cfg.Identity, cfg.Server.ID)
	if err != nil {
		return nil, err
	}

	dialCtx, cancel := context.WithTimeout(ctx, defaultQUICDialTimeout)
	defer cancel()

	conn, err := quic.DialAddr(dialCtx, cfg.Server.Address, tlsConfig, &quic.Config{})
	if err != nil {
		return nil, err
	}

	if logger != nil {
		logger.Info("client connected", "node_id", cfg.NodeID, "server_id", cfg.Server.ID, "addr", cfg.Server.Address)
	}

	return conn, nil
}

func OpenTCPStream(ctx context.Context, conn *quic.Conn, target string) (*quic.Stream, error) {
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}

	if err := protocol.WriteConnectTCPRequest(stream, protocol.ConnectTCPRequest{Target: target}); err != nil {
		_ = stream.Close()
		return nil, err
	}

	resp, err := protocol.ReadConnectTCPResponse(stream)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	if !resp.OK {
		_ = stream.Close()
		if resp.Message == "" {
			return nil, fmt.Errorf("connect tcp failed")
		}
		return nil, fmt.Errorf("connect tcp failed: %s", resp.Message)
	}

	return stream, nil
}

func ConnectTCP(ctx context.Context, cfg *config.Config, target string, logger *slog.Logger) (*quic.Conn, *quic.Stream, error) {
	conn, err := Connect(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}

	stream, err := OpenTCPStream(ctx, conn, target)
	if err != nil {
		_ = conn.CloseWithError(0, "open tcp stream failed")
		return nil, nil, err
	}

	return conn, stream, nil
}
