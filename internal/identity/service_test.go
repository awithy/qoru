package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/awithy/qoru/internal/config"
)

func TestServiceNameFromCertificateUsesSPIFFEURISAN(t *testing.T) {
	uri, err := url.Parse("spiffe://qoru/service/echo")
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{URIs: []*url.URL{uri}, DNSNames: []string{"ignored"}, Subject: pkix.Name{CommonName: "ignored"}}

	service, err := ServiceNameFromCertificate(cert)
	if err != nil {
		t.Fatalf("expected service name, got %v", err)
	}
	if service != "echo" {
		t.Fatalf("expected echo, got %q", service)
	}
}

func TestServiceNameFromCertificateRejectsNodeIdentity(t *testing.T) {
	uri, err := url.Parse("spiffe://qoru/node/relay-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceNameFromCertificate(&x509.Certificate{URIs: []*url.URL{uri}}); err == nil {
		t.Fatal("expected node identity to be rejected")
	}
}

func TestServiceNameFromCertificateRejectsDNSAndCNOnly(t *testing.T) {
	cert := &x509.Certificate{DNSNames: []string{"echo"}, Subject: pkix.Name{CommonName: "echo"}}
	if _, err := ServiceNameFromCertificate(cert); err == nil {
		t.Fatal("expected DNS/CN-only identity to be rejected")
	}
}

func TestServiceNameFromCertificateRejectsNilCertificate(t *testing.T) {
	if _, err := ServiceNameFromCertificate(nil); err == nil {
		t.Fatal("expected nil certificate to be rejected")
	}
}

func TestVerifyServiceCertificate(t *testing.T) {
	caCert, caKey, caPEM := makeTestCA(t)
	serviceDER, _, _ := makeTestLeafCert(t, caCert, caKey, "spiffe://qoru/service/echo")
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("append test ca")
	}

	if err := VerifyServiceCertificate([][]byte{serviceDER}, roots, "echo"); err != nil {
		t.Fatalf("expected service certificate to verify, got %v", err)
	}
}

func TestVerifyServiceCertificateRejectsWrongService(t *testing.T) {
	caCert, caKey, caPEM := makeTestCA(t)
	serviceDER, _, _ := makeTestLeafCert(t, caCert, caKey, "spiffe://qoru/service/echo")
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("append test ca")
	}

	if err := VerifyServiceCertificate([][]byte{serviceDER}, roots, "postgres"); err == nil {
		t.Fatal("expected wrong service to be rejected")
	}
}

func TestVerifyServiceCertificateRejectsNodeCertificate(t *testing.T) {
	caCert, caKey, caPEM := makeTestCA(t)
	nodeDER, _, _ := makeTestLeafCert(t, caCert, caKey, "spiffe://qoru/node/relay-b")
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("append test ca")
	}

	if err := VerifyServiceCertificate([][]byte{nodeDER}, roots, "echo"); err == nil {
		t.Fatal("expected node certificate to be rejected")
	}
}

func TestLoadServiceCertificateAndCertPool(t *testing.T) {
	caCert, caKey, caPEM := makeTestCA(t)
	_, certPEM, keyPEM := makeTestLeafCert(t, caCert, caKey, "spiffe://qoru/service/echo")
	dir := t.TempDir()
	certPath := filepath.Join(dir, "echo.crt")
	keyPath := filepath.Join(dir, "echo.key")
	caPath := filepath.Join(dir, "service-ca.crt")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	cert, err := LoadServiceCertificate(config.ServiceE2EConfig{Cert: certPath, Key: keyPath})
	if err != nil {
		t.Fatalf("LoadServiceCertificate returned error: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("expected loaded certificate chain")
	}
	pool, err := LoadCertPool(caPath)
	if err != nil {
		t.Fatalf("LoadCertPool returned error: %v", err)
	}
	if pool == nil {
		t.Fatal("expected cert pool")
	}
}

func makeTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "qoru-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func makeTestLeafCert(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, uriValue string) ([]byte, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse(uriValue)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "qoru-test-leaf"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		URIs:                  []*url.URL{uri},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return der, certPEM, keyPEM
}
