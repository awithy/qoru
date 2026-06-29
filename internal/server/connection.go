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

const defaultTCPDialTimeout = 10 * time.Second

func handleConnection(ctx context.Context, cfg *config.Config, conn *quic.Conn, logger *slog.Logger, opts options) {
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
		defer logger.Info("peer disconnected", "peer_id", peerID)
	}

	var streamWG sync.WaitGroup
	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			if ctx.Err() == nil && logger != nil {
				logger.Error("accept stream failed", "error", err)
			}
			_ = conn.CloseWithError(0, "done")
			if waitErr := waitGroupTimeout(&streamWG, defaultShutdownWaitTimeout); waitErr != nil && logger != nil {
				logger.Warn("timed out waiting for streams to close", "peer_id", peerID, "error", waitErr)
			}
			return
		}
		streamWG.Add(1)
		go func() {
			defer streamWG.Done()
			handleStream(ctx, cfg, peerID, stream, logger, opts)
		}()
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
		logger.Info("connect requested", "peer_id", peerID, "protocol", req.Protocol, "service", req.Service, "egress", req.Egress)
	}
	if opts.connectRequest != nil {
		opts.connectRequest(req)
	}

	if req.Protocol != "tcp" {
		err := fmt.Errorf("unsupported connect protocol %q", req.Protocol)
		if logger != nil {
			logger.Warn("connect protocol unsupported", "peer_id", peerID, "protocol", req.Protocol, "service", req.Service, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Message: err.Error()})
		_ = stream.Close()
		return
	}

	if req.Egress != "" && req.Egress != cfg.NodeID {
		err := fmt.Errorf("egress %q is not reachable from one-hop server %q", req.Egress, cfg.NodeID)
		if logger != nil {
			logger.Warn("egress unsupported", "peer_id", peerID, "egress", req.Egress, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Message: err.Error()})
		_ = stream.Close()
		return
	}

	svc, err := resolveService(cfg, peerID, req.Protocol, req.Service)
	if err != nil {
		if logger != nil {
			logger.Warn("service denied", "peer_id", peerID, "protocol", req.Protocol, "service", req.Service, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Message: err.Error()})
		_ = stream.Close()
		return
	}

	targetConn, err := dialTCP(ctx, svc.Target)
	if err != nil {
		if logger != nil {
			logger.Error("tcp target dial failed", "peer_id", peerID, "service", svc.Name, "target", svc.Target, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Message: err.Error()})
		_ = stream.Close()
		return
	}
	defer targetConn.Close()

	if err := protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: true}); err != nil {
		if logger != nil {
			logger.Error("write connect tcp response failed", "peer_id", peerID, "service", svc.Name, "target", svc.Target, "error", err)
		}
		_ = stream.Close()
		return
	}

	if logger != nil {
		logger.Info("tcp target connected", "peer_id", peerID, "service", svc.Name, "target", svc.Target)
	}

	proxyTCP(stream, targetConn)

	if logger != nil {
		logger.Info("tcp proxy closed", "peer_id", peerID, "service", svc.Name, "target", svc.Target)
	}
}
