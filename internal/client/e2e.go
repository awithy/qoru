package client

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
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

type e2eHandshakePhaseError struct {
	phase string
	err   error
}

func (e *e2eHandshakePhaseError) Error() string {
	return e.err.Error()
}

func (e *e2eHandshakePhaseError) Unwrap() error {
	return e.err
}

func e2ePhaseError(phase string, err error) error {
	if err == nil {
		return nil
	}
	return &e2eHandshakePhaseError{phase: phase, err: err}
}

func e2eErrorPhase(err error) string {
	var phaseErr *e2eHandshakePhaseError
	if errors.As(err, &phaseErr) {
		return phaseErr.phase
	}
	return ""
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
		return nil, nil, e2ePhaseError("generate_client_ephemeral", err)
	}
	clientHello, err := e2e.NewClientHello(handshakeCtx, rt.cert, clientEphemeral.Public)
	if err != nil {
		return nil, nil, e2ePhaseError("sign_client_hello", err)
	}
	if err := protocol.WriteE2EClientHello(stream, clientHello); err != nil {
		return nil, nil, e2ePhaseError("write_client_hello", fmt.Errorf("write e2e client hello: %w", err))
	}
	serverHello, err := readE2EServerHelloOrClose(stream)
	if err != nil {
		return nil, nil, e2ePhaseError("read_server_hello", err)
	}
	if _, err := e2e.VerifyServerHello(handshakeCtx, serverHello, rt.serviceRoots, clientHello.EphemeralPublicKey); err != nil {
		return nil, nil, e2ePhaseError("verify_server_hello", err)
	}
	finished, err := e2e.NewClientFinished(handshakeCtx, rt.cert, clientHello.EphemeralPublicKey, serverHello.EphemeralPublicKey)
	if err != nil {
		return nil, nil, e2ePhaseError("sign_client_finished", err)
	}
	if err := protocol.WriteE2EClientFinished(stream, finished); err != nil {
		return nil, nil, e2ePhaseError("write_client_finished", fmt.Errorf("write e2e client finished: %w", err))
	}
	sharedSecret, err := e2e.SharedSecret(clientEphemeral.Private, serverHello.EphemeralPublicKey)
	if err != nil {
		return nil, nil, e2ePhaseError("derive_shared_secret", err)
	}
	transcriptHash, err := e2e.TranscriptHash(handshakeCtx, clientHello.EphemeralPublicKey, serverHello.EphemeralPublicKey)
	if err != nil {
		return nil, nil, e2ePhaseError("hash_transcript", err)
	}
	keys, err := e2e.DeriveTrafficKeys(sharedSecret, transcriptHash)
	if err != nil {
		return nil, nil, e2ePhaseError("derive_traffic_keys", err)
	}
	reader, err := e2e.NewEncryptedReader(stream, keys.ServerToClient, transcriptHash)
	if err != nil {
		return nil, nil, e2ePhaseError("create_encrypted_reader", err)
	}
	writer, err := e2e.NewEncryptedWriter(stream, keys.ClientToServer, transcriptHash)
	if err != nil {
		return nil, nil, e2ePhaseError("create_encrypted_writer", err)
	}
	logger.Info("e2e handshake complete")
	return reader, writer, nil
}

func readE2EServerHelloOrClose(r io.Reader) (protocol.E2EServerHello, error) {
	frame, err := protocol.ReadFrame(r)
	if err != nil {
		return protocol.E2EServerHello{}, fmt.Errorf("read e2e server hello: %w", err)
	}
	switch frame.Type {
	case protocol.TypeE2EServerHello:
		return protocol.DecodeE2EServerHelloPayload(frame.Payload)
	case protocol.TypeE2EClose:
		closeFrame, err := protocol.DecodeE2EClosePayload(frame.Payload)
		if err != nil {
			return protocol.E2EServerHello{}, err
		}
		return protocol.E2EServerHello{}, &e2e.CloseError{Code: closeFrame.Code, ConnectCode: closeFrame.ConnectCode, Message: closeFrame.Message}
	default:
		return protocol.E2EServerHello{}, fmt.Errorf("unexpected message type %d", frame.Type)
	}
}
