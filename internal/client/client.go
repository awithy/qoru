package client

import (
	"context"
	"log/slog"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/identity"
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

	return nil
}
