package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/identity"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/quic-go/quic-go"
)

const defaultTCPDialTimeout = 10 * time.Second

func handleConnection(ctx context.Context, cfg *config.Config, conn *quic.Conn, logger *slog.Logger, opts options) {
	defer conn.CloseWithError(0, "done")

	peerID, err := identity.PeerNodeID(conn.ConnectionState().TLS)
	if err != nil {
		if logger != nil {
			logger.Error("extract peer identity failed", "error", err)
		}
		_ = conn.CloseWithError(1, "peer identity required")
		return
	}
	if logger != nil {
		logger.Info("peer connected", "peer_id", peerID)
	}

	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			if ctx.Err() == nil && logger != nil {
				logger.Error("accept stream failed", "error", err)
			}
			return
		}
		go handleStream(ctx, cfg, peerID, stream, logger, opts)
	}
}

func handleStream(ctx context.Context, cfg *config.Config, peerID string, stream *quic.Stream, logger *slog.Logger, opts options) {
	req, err := protocol.ReadConnectRequest(stream)
	if err != nil {
		if logger != nil {
			logger.Error("read connect request failed", "error", err)
		}
		_ = stream.Close()
		return
	}

	if logger != nil {
		logger.Info("connect requested", "peer_id", peerID, "protocol", req.Protocol, "target", req.Target)
	}
	if opts.connectRequest != nil {
		opts.connectRequest(req)
	}

	if req.Protocol != "tcp" {
		err := fmt.Errorf("unsupported connect protocol %q", req.Protocol)
		if logger != nil {
			logger.Warn("connect protocol unsupported", "peer_id", peerID, "protocol", req.Protocol, "target", req.Target, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Message: err.Error()})
		_ = stream.Close()
		return
	}

	if err := authorizeTCPTarget(cfg, peerID, req.Target); err != nil {
		if logger != nil {
			logger.Warn("tcp target denied", "peer_id", peerID, "target", req.Target, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Message: err.Error()})
		_ = stream.Close()
		return
	}

	targetConn, err := dialTCP(ctx, req.Target)
	if err != nil {
		if logger != nil {
			logger.Error("tcp target dial failed", "peer_id", peerID, "target", req.Target, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Message: err.Error()})
		_ = stream.Close()
		return
	}
	defer targetConn.Close()

	if err := protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: true}); err != nil {
		if logger != nil {
			logger.Error("write connect tcp response failed", "peer_id", peerID, "target", req.Target, "error", err)
		}
		_ = stream.Close()
		return
	}

	if logger != nil {
		logger.Info("tcp target connected", "peer_id", peerID, "target", req.Target)
	}

	proxyTCP(stream, targetConn)
}
