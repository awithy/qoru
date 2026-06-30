package identity

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/awithy/qoru/internal/config"
)

const spiffeServicePrefix = "spiffe://qoru/service/"

func ServiceNameFromCertificate(cert *x509.Certificate) (string, error) {
	if cert == nil {
		return "", fmt.Errorf("service certificate is required")
	}
	for _, uri := range cert.URIs {
		value := uri.String()
		if strings.HasPrefix(value, spiffeServicePrefix) {
			service := strings.TrimPrefix(value, spiffeServicePrefix)
			if service != "" {
				return service, nil
			}
		}
	}
	return "", fmt.Errorf("service certificate does not contain a SPIFFE service identity")
}

func VerifyServiceCertificate(rawCerts [][]byte, roots *x509.CertPool, expectedService string) error {
	if expectedService == "" {
		return fmt.Errorf("expected service is required")
	}
	if len(rawCerts) == 0 {
		return fmt.Errorf("service certificate is required")
	}
	if roots == nil {
		return fmt.Errorf("service CA roots are required")
	}

	certs := make([]*x509.Certificate, 0, len(rawCerts))
	for _, raw := range rawCerts {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return fmt.Errorf("parse service certificate: %w", err)
		}
		certs = append(certs, cert)
	}

	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	if _, err := certs[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return fmt.Errorf("verify service certificate chain: %w", err)
	}

	service, err := ServiceNameFromCertificate(certs[0])
	if err != nil {
		return err
	}
	if service != expectedService {
		return fmt.Errorf("service identity mismatch: expected %q, got %q", expectedService, service)
	}
	return nil
}

func LoadServiceCertificate(e2e config.ServiceE2EConfig) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(e2e.Cert, e2e.Key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load service certificate/key: %w", err)
	}
	return cert, nil
}

func LoadCertPool(caPath string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read ca certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse ca certificate: no certificates found")
	}
	return pool, nil
}
