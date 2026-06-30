package server

import (
	"crypto/x509"
	"fmt"
	"log/slog"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/e2e"
	"github.com/awithy/qoru/internal/identity"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/quic-go/quic-go"
)

type e2eServerRuntime struct {
	nodeRoots *x509.CertPool
}

func newE2EServerRuntime(cfg *config.Config) (*e2eServerRuntime, error) {
	roots, err := identity.LoadCertPool(cfg.Identity.CA)
	if err != nil {
		return nil, fmt.Errorf("load node identity CA: %w", err)
	}
	return &e2eServerRuntime{nodeRoots: roots}, nil
}

type e2eServerHandshakeResult struct {
	reader   *e2e.EncryptedReader
	writer   *e2e.EncryptedWriter
	clientID string
}

func (rt *e2eServerRuntime) runHandshake(stream *quic.Stream, req protocol.ConnectRequest, svc config.ServiceConfig, logger *slog.Logger) (e2eServerHandshakeResult, error) {
	if rt == nil {
		return e2eServerHandshakeResult{}, fmt.Errorf("e2e runtime is not initialized")
	}
	if svc.E2E.Cert == "" || svc.E2E.Key == "" {
		return e2eServerHandshakeResult{}, fmt.Errorf("service %q is not configured for e2e", svc.Name)
	}
	serviceCert, err := identity.LoadServiceCertificate(svc.E2E)
	if err != nil {
		return e2eServerHandshakeResult{}, err
	}
	transcriptRoute := req.E2ERoute
	if transcriptRoute == nil {
		transcriptRoute = req.Route
	}
	handshakeCtx := e2e.Context{RequestID: req.RequestID, Service: req.Service, Egress: req.Egress, Route: transcriptRoute}
	clientHello, err := protocol.ReadE2EClientHello(stream)
	if err != nil {
		return e2eServerHandshakeResult{}, fmt.Errorf("read e2e client hello: %w", err)
	}
	clientID, clientLeaf, err := e2e.VerifyClientHello(handshakeCtx, clientHello, rt.nodeRoots)
	if err != nil {
		return e2eServerHandshakeResult{}, err
	}
	if err := authorizeServicePeer(svc, clientID, req.Protocol, req.Service); err != nil {
		return e2eServerHandshakeResult{}, err
	}
	serverEphemeral, err := e2e.GenerateEphemeralKey(nil)
	if err != nil {
		return e2eServerHandshakeResult{}, err
	}
	serverHello, err := e2e.NewServerHello(handshakeCtx, serviceCert, clientHello.EphemeralPublicKey, serverEphemeral.Public)
	if err != nil {
		return e2eServerHandshakeResult{}, err
	}
	if err := protocol.WriteE2EServerHello(stream, serverHello); err != nil {
		return e2eServerHandshakeResult{}, fmt.Errorf("write e2e server hello: %w", err)
	}
	finished, err := protocol.ReadE2EClientFinished(stream)
	if err != nil {
		return e2eServerHandshakeResult{}, fmt.Errorf("read e2e client finished: %w", err)
	}
	if err := e2e.VerifyClientFinished(handshakeCtx, finished, clientLeaf, clientHello.EphemeralPublicKey, serverHello.EphemeralPublicKey); err != nil {
		return e2eServerHandshakeResult{}, err
	}
	sharedSecret, err := e2e.SharedSecret(serverEphemeral.Private, clientHello.EphemeralPublicKey)
	if err != nil {
		return e2eServerHandshakeResult{}, err
	}
	transcriptHash, err := e2e.TranscriptHash(handshakeCtx, clientHello.EphemeralPublicKey, serverHello.EphemeralPublicKey)
	if err != nil {
		return e2eServerHandshakeResult{}, err
	}
	keys, err := e2e.DeriveTrafficKeys(sharedSecret, transcriptHash)
	if err != nil {
		return e2eServerHandshakeResult{}, err
	}
	reader, err := e2e.NewEncryptedReader(stream, keys.ClientToServer, transcriptHash)
	if err != nil {
		return e2eServerHandshakeResult{}, err
	}
	writer, err := e2e.NewEncryptedWriter(stream, keys.ServerToClient, transcriptHash)
	if err != nil {
		return e2eServerHandshakeResult{}, err
	}
	logger.Info("e2e handshake complete", "original_client_id", clientID)
	return e2eServerHandshakeResult{reader: reader, writer: writer, clientID: clientID}, nil
}

func isServiceE2EConfigured(svc config.ServiceConfig) bool {
	return svc.E2E.Cert != "" || svc.E2E.Key != ""
}

func writeE2ECloseError(stream *quic.Stream, err error) {
	if err == nil {
		return
	}
	_ = protocol.WriteE2EClose(stream, protocol.E2EClose{Code: e2e.CloseCodeError, Message: err.Error()})
}
