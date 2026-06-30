package server

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

type e2eServerRuntime struct {
	nodeRoots    *x509.CertPool
	serviceCerts map[string]tls.Certificate
}

func newE2EServerRuntime(cfg *config.Config) (*e2eServerRuntime, error) {
	roots, err := identity.LoadCertPool(cfg.Identity.CA)
	if err != nil {
		return nil, fmt.Errorf("load node identity CA: %w", err)
	}
	rt := &e2eServerRuntime{nodeRoots: roots, serviceCerts: make(map[string]tls.Certificate)}
	if err := rt.loadServiceCertificates(cfg); err != nil {
		return nil, err
	}
	return rt, nil
}

func (rt *e2eServerRuntime) loadServiceCertificates(cfg *config.Config) error {
	var serviceRoots *x509.CertPool
	for i, svc := range cfg.Services {
		if !isServiceE2EConfigured(svc) {
			continue
		}
		if serviceRoots == nil {
			roots, err := identity.LoadCertPool(cfg.ServiceIdentity.CA)
			if err != nil {
				return fmt.Errorf("load service identity CA: %w", err)
			}
			serviceRoots = roots
		}
		cert, err := identity.LoadServiceCertificate(svc.E2E)
		if err != nil {
			return fmt.Errorf("load services[%d] e2e certificate: %w", i, err)
		}
		if err := identity.VerifyServiceCertificate(cert.Certificate, serviceRoots, svc.Name); err != nil {
			return fmt.Errorf("verify services[%d] e2e certificate for service %q: %w", i, svc.Name, err)
		}
		rt.serviceCerts[serviceE2ECacheKey(svc)] = cert
	}
	return nil
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
	serviceCert, err := rt.serviceCertificate(svc)
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

func (rt *e2eServerRuntime) serviceCertificate(svc config.ServiceConfig) (tls.Certificate, error) {
	if svc.E2E.Cert == "" || svc.E2E.Key == "" {
		return tls.Certificate{}, fmt.Errorf("service %q is not configured for e2e", svc.Name)
	}
	cert, ok := rt.serviceCerts[serviceE2ECacheKey(svc)]
	if !ok {
		return tls.Certificate{}, fmt.Errorf("service %q e2e certificate is not loaded", svc.Name)
	}
	return cert, nil
}

func serviceE2ECacheKey(svc config.ServiceConfig) string {
	return svc.Protocol + "\x00" + svc.Name + "\x00" + svc.E2E.Cert + "\x00" + svc.E2E.Key
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
