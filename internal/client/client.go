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

	tlsConfig, err := identity.ClientTLSConfig(cfg.Identity, cfg.Server.ID)
	if err != nil {
		return err
	}

	conn, err := quic.DialAddr(ctx, cfg.Server.Address, tlsConfig, &quic.Config{})
	if err != nil {
		return err
	}
	defer conn.CloseWithError(0, "done")

	if logger != nil {
		logger.Info("client connected", "node_id", cfg.NodeID, "server_id", cfg.Server.ID, "addr", cfg.Server.Address)
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return err
	}

	if err := protocol.WriteConnectTCPRequest(stream, protocol.ConnectTCPRequest{Target: cfg.TCPForwards[0].Target}); err != nil {
		_ = stream.Close()
		return err
	}
	if err := stream.Close(); err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, stream)
	return nil
}
