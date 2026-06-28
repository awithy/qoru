package server

import (
	"context"
	"log/slog"
	"net"

	"github.com/awithy/qoru/internal/protocol"
	"github.com/quic-go/quic-go"
)

func handleConnection(ctx context.Context, conn *quic.Conn, logger *slog.Logger, opts options) {
	defer conn.CloseWithError(0, "done")

	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		if ctx.Err() == nil && logger != nil {
			logger.Error("accept stream failed", "error", err)
		}
		return
	}
	defer stream.Close()

	req, err := protocol.ReadConnectTCPRequest(stream)
	if err != nil {
		if logger != nil {
			logger.Error("read connect tcp request failed", "error", err)
		}
		return
	}

	if logger != nil {
		logger.Info("connect tcp requested", "target", req.Target)
	}
	if opts.connectTCPRequest != nil {
		opts.connectTCPRequest(req)
	}

	targetConn, err := net.Dial("tcp", req.Target)
	if err != nil {
		if logger != nil {
			logger.Error("tcp target dial failed", "target", req.Target, "error", err)
		}
		return
	}
	defer targetConn.Close()

	if logger != nil {
		logger.Info("tcp target connected", "target", req.Target)
	}
}
