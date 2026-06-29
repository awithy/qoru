package client

import (
	"context"
	"log/slog"
	"net"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/identity"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/quic-go/quic-go"
)

type options struct {
	started func(addr string)
}

type Option func(*options)

func WithStartedFunc(fn func(addr string)) Option {
	return func(opts *options) {
		opts.started = fn
	}
}

func Run(ctx context.Context, cfg *config.Config, logger *slog.Logger, runOptions ...Option) error {
	if err := config.ValidateClient(cfg); err != nil {
		return err
	}

	opts := options{}
	for _, apply := range runOptions {
		apply(&opts)
	}

	forward := cfg.TCPForwards[0]
	listener, err := net.Listen("tcp", forward.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()

	addr := listener.Addr().String()
	if logger != nil {
		logger.Info("client listening", "node_id", cfg.NodeID, "addr", addr, "target", forward.Target)
	}
	if opts.started != nil {
		opts.started(addr)
	}

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		localConn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go handleLocalConnection(ctx, cfg, forward.Target, localConn, logger)
	}
}

func handleLocalConnection(ctx context.Context, cfg *config.Config, target string, localConn net.Conn, logger *slog.Logger) {
	defer localConn.Close()

	conn, stream, err := ConnectTCP(ctx, cfg, target, logger)
	if err != nil {
		if logger != nil {
			logger.Error("connect tcp failed", "target", target, "error", err)
		}
		return
	}
	defer conn.CloseWithError(0, "done")

	proxyTCP(localConn, stream)
}

func ConnectTCP(ctx context.Context, cfg *config.Config, target string, logger *slog.Logger) (*quic.Conn, *quic.Stream, error) {
	if err := config.ValidateClient(cfg); err != nil {
		return nil, nil, err
	}

	tlsConfig, err := identity.ClientTLSConfig(cfg.Identity, cfg.Server.ID)
	if err != nil {
		return nil, nil, err
	}

	conn, err := quic.DialAddr(ctx, cfg.Server.Address, tlsConfig, &quic.Config{})
	if err != nil {
		return nil, nil, err
	}

	if logger != nil {
		logger.Info("client connected", "node_id", cfg.NodeID, "server_id", cfg.Server.ID, "addr", cfg.Server.Address)
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		_ = conn.CloseWithError(0, "open stream failed")
		return nil, nil, err
	}

	if err := protocol.WriteConnectTCPRequest(stream, protocol.ConnectTCPRequest{Target: target}); err != nil {
		_ = stream.Close()
		_ = conn.CloseWithError(0, "write connect tcp request failed")
		return nil, nil, err
	}

	return conn, stream, nil
}
