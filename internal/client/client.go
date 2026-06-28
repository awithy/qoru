package client

import (
	"context"
	"io"
	"log/slog"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/identity"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/quic-go/quic-go"
)

func Run(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	if err := config.ValidateClient(cfg); err != nil {
		return err
	}

	conn, stream, err := ConnectTCP(ctx, cfg, cfg.TCPForwards[0].Target, logger)
	if err != nil {
		return err
	}
	defer conn.CloseWithError(0, "done")

	if err := stream.Close(); err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, stream)
	return nil
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
