package client

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/identity"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/quic-go/quic-go"
)

const defaultQUICDialTimeout = 10 * time.Second

type ReconnectBackoffError struct {
	ServerID    string
	Address     string
	NextAttempt time.Time
	Err         error
}

func (e *ReconnectBackoffError) Error() string {
	msg := "upstream reconnect backoff active until " + e.NextAttempt.Format(time.RFC3339Nano)
	if e.Err != nil {
		return msg + ": " + e.Err.Error()
	}
	return msg
}

func (e *ReconnectBackoffError) Unwrap() error {
	return e.Err
}

type ConnectRejectedError struct {
	Code    protocol.ConnectCode
	Message string
}

func (e *ConnectRejectedError) Error() string {
	prefix := "connect rejected"
	if e.Code != protocol.ConnectCodeOK {
		prefix += ": " + e.Code.String()
	}
	if e.Message == "" {
		return prefix
	}
	return prefix + ": " + e.Message
}

func isConnectRejected(err error) bool {
	var rejected *ConnectRejectedError
	return errors.As(err, &rejected)
}

func ConnectToServer(ctx context.Context, nodeID string, identityCfg config.IdentityConfig, serverCfg config.ServerConfig, logger *slog.Logger) (*quic.Conn, error) {
	logger = ensureLogger(logger)
	tlsConfig, err := identity.ClientTLSConfig(identityCfg, serverCfg.ID)
	if err != nil {
		return nil, err
	}

	dialCtx, cancel := context.WithTimeout(ctx, defaultQUICDialTimeout)
	defer cancel()

	conn, err := quic.DialAddr(dialCtx, serverCfg.Address, tlsConfig, &quic.Config{})
	if err != nil {
		return nil, err
	}

	logger.Info("client connected", "node_id", nodeID, "server_id", serverCfg.ID, "addr", serverCfg.Address)

	return conn, nil
}

func OpenTCPStream(ctx context.Context, conn *quic.Conn, requestID, service, egress string, route []string) (*quic.Stream, error) {
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}

	if err := protocol.WriteConnectRequest(stream, protocol.ConnectRequest{RequestID: requestID, Protocol: "tcp", Service: service, Egress: egress, Route: route}); err != nil {
		_ = stream.Close()
		return nil, err
	}

	resp, err := protocol.ReadConnectResponse(stream)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	if !resp.OK {
		_ = stream.Close()
		return nil, &ConnectRejectedError{Code: resp.Code, Message: resp.Message}
	}

	return stream, nil
}
