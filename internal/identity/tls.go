package identity

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/awithy/qoru/internal/config"
)

const ALPN = "qoru/1"

func ServerTLSConfig(identity config.IdentityConfig) (*tls.Config, error) {
	cert, caPool, err := loadCertificateAndCA(identity)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
		NextProtos:   []string{ALPN},
	}, nil
}

func ClientTLSConfig(identity config.IdentityConfig, serverName string) (*tls.Config, error) {
	cert, caPool, err := loadCertificateAndCA(identity)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caPool,
		InsecureSkipVerify: true, // verified by VerifyPeerCertificate using qoru node URI SAN identity.
		NextProtos:         []string{ALPN},
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			return verifyPeerCertificate(rawCerts, caPool, serverName, x509.ExtKeyUsageServerAuth)
		},
	}, nil
}

func verifyPeerCertificate(rawCerts [][]byte, roots *x509.CertPool, expectedNodeID string, keyUsage x509.ExtKeyUsage) error {
	if len(rawCerts) == 0 {
		return fmt.Errorf("peer certificate is required")
	}

	certs := make([]*x509.Certificate, 0, len(rawCerts))
	for _, raw := range rawCerts {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return fmt.Errorf("parse peer certificate: %w", err)
		}
		certs = append(certs, cert)
	}

	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}

	if _, err := certs[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{keyUsage}}); err != nil {
		return fmt.Errorf("verify peer certificate chain: %w", err)
	}

	nodeID, err := PeerNodeID(tls.ConnectionState{PeerCertificates: certs})
	if err != nil {
		return err
	}
	if nodeID != expectedNodeID {
		return fmt.Errorf("peer node identity mismatch: expected %q, got %q", expectedNodeID, nodeID)
	}

	return nil
}

func loadCertificateAndCA(identity config.IdentityConfig) (tls.Certificate, *x509.CertPool, error) {
	cert, err := tls.LoadX509KeyPair(identity.Cert, identity.Key)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("load certificate/key: %w", err)
	}

	caPEM, err := os.ReadFile(identity.CA)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("read ca certificate: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return tls.Certificate{}, nil, fmt.Errorf("parse ca certificate: no certificates found")
	}

	return cert, caPool, nil
}
