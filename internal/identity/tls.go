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
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		ServerName:   serverName,
		NextProtos:   []string{ALPN},
	}, nil
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
