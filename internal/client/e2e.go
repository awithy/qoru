package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/e2e"
	"github.com/awithy/qoru/internal/identity"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/quic-go/quic-go"
)

type e2eClientRuntime struct {
	cert         tls.Certificate
	serviceRoots *x509.CertPool
}

func newE2EClientRuntime(cfg *config.Config) (*e2eClientRuntime, error) {
	if cfg.ServiceIdentity.CA == "" {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(cfg.Identity.Cert, cfg.Identity.Key)
	if err != nil {
		return nil, fmt.Errorf("load client e2e certificate/key: %w", err)
	}
	roots, err := identity.LoadCertPool(cfg.ServiceIdentity.CA)
	if err != nil {
		return nil, fmt.Errorf("load service identity CA: %w", err)
	}
	return &e2eClientRuntime{cert: cert, serviceRoots: roots}, nil
}

func (rt *e2eClientRuntime) runHandshake(stream *quic.Stream, requestID string, candidate selectedRoute, logger *slog.Logger) (*e2e.EncryptedReader, *e2e.EncryptedWriter, error) {
	if rt == nil {
		return nil, nil, fmt.Errorf("service_identity.ca is required for e2e handshake")
	}
	handshakeCtx := e2e.Context{RequestID: requestID, Service: candidate.service, Egress: candidate.egress, Route: candidate.route}
	clientEphemeral, err := e2e.GenerateEphemeralKey(nil)
	if err != nil {
		return nil, nil, err
	}
	clientHello, err := e2e.NewClientHello(handshakeCtx, rt.cert, clientEphemeral.Public)
	if err != nil {
		return nil, nil, err
	}
	if err := protocol.WriteE2EClientHello(stream, clientHello); err != nil {
		return nil, nil, fmt.Errorf("write e2e client hello: %w", err)
	}
	serverHello, err := protocol.ReadE2EServerHello(stream)
	if err != nil {
		return nil, nil, fmt.Errorf("read e2e server hello: %w", err)
	}
	if _, err := e2e.VerifyServerHello(handshakeCtx, serverHello, rt.serviceRoots, clientHello.EphemeralPublicKey); err != nil {
		return nil, nil, err
	}
	finished, err := e2e.NewClientFinished(handshakeCtx, rt.cert, clientHello.EphemeralPublicKey, serverHello.EphemeralPublicKey)
	if err != nil {
		return nil, nil, err
	}
	if err := protocol.WriteE2EClientFinished(stream, finished); err != nil {
		return nil, nil, fmt.Errorf("write e2e client finished: %w", err)
	}
	sharedSecret, err := e2e.SharedSecret(clientEphemeral.Private, serverHello.EphemeralPublicKey)
	if err != nil {
		return nil, nil, err
	}
	transcriptHash, err := e2e.TranscriptHash(handshakeCtx, clientHello.EphemeralPublicKey, serverHello.EphemeralPublicKey)
	if err != nil {
		return nil, nil, err
	}
	keys, err := e2e.DeriveTrafficKeys(sharedSecret, transcriptHash)
	if err != nil {
		return nil, nil, err
	}
	reader, err := e2e.NewEncryptedReader(stream, keys.ServerToClient, transcriptHash)
	if err != nil {
		return nil, nil, err
	}
	writer, err := e2e.NewEncryptedWriter(stream, keys.ClientToServer, transcriptHash)
	if err != nil {
		return nil, nil, err
	}
	logger.Info("e2e handshake complete")
	return reader, writer, nil
}
