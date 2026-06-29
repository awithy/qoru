package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/awithy/qoru/internal/protocol"
	"github.com/quic-go/quic-go"
)

const defaultTCPDialTimeout = 10 * time.Second

func handleConnection(ctx context.Context, conn *quic.Conn, logger *slog.Logger, opts options) {
	defer conn.CloseWithError(0, "done")

	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			if ctx.Err() == nil && logger != nil {
				logger.Error("accept stream failed", "error", err)
			}
			return
		}
		go handleStream(ctx, stream, logger, opts)
	}
}

func handleStream(ctx context.Context, stream *quic.Stream, logger *slog.Logger, opts options) {
	req, err := protocol.ReadConnectTCPRequest(stream)
	if err != nil {
		if logger != nil {
			logger.Error("read connect tcp request failed", "error", err)
		}
		_ = stream.Close()
		return
	}

	if logger != nil {
		logger.Info("connect tcp requested", "target", req.Target)
	}
	if opts.connectTCPRequest != nil {
		opts.connectTCPRequest(req)
	}

	targetConn, err := dialTCP(ctx, req.Target)
	if err != nil {
		if logger != nil {
			logger.Error("tcp target dial failed", "target", req.Target, "error", err)
		}
		_ = protocol.WriteConnectTCPResponse(stream, protocol.ConnectTCPResponse{OK: false, Message: err.Error()})
		_ = stream.Close()
		return
	}
	defer targetConn.Close()

	if err := protocol.WriteConnectTCPResponse(stream, protocol.ConnectTCPResponse{OK: true}); err != nil {
		if logger != nil {
			logger.Error("write connect tcp response failed", "target", req.Target, "error", err)
		}
		_ = stream.Close()
		return
	}

	if logger != nil {
		logger.Info("tcp target connected", "target", req.Target)
	}

	proxyTCP(stream, targetConn)
}
