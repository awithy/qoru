package testcert

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
	"sync"
	"testing"
	"time"

	"github.com/awithy/qoru/internal/config"
)

type bundle struct {
	dir               string
	caCert            *x509.Certificate
	caKey             *ecdsa.PrivateKey
	caCertPath        string
	serviceCACert     *x509.Certificate
	serviceCAKey      *ecdsa.PrivateKey
	serviceCACertPath string

	mu      sync.Mutex
	nodes   map[string]config.IdentityConfig
	service map[string]config.ServiceE2EConfig
}

var (
	once    sync.Once
	shared  *bundle
	onceErr error
)

func NodeIdentity(tb testing.TB, nodeID string) config.IdentityConfig {
	tb.Helper()
	b := mustBundle(tb)
	identity, err := b.nodeIdentity(nodeID)
	if err != nil {
		tb.Fatalf("generate node test certificate %q: %v", nodeID, err)
	}
	return identity
}

func MustNodeIdentity(nodeID string) config.IdentityConfig {
	b := mustBundlePanic()
	identity, err := b.nodeIdentity(nodeID)
	if err != nil {
		panic(err)
	}
	return identity
}

func CAPath(tb testing.TB) string {
	tb.Helper()
	return mustBundle(tb).caCertPath
}

func ServiceCAPath(tb testing.TB) string {
	tb.Helper()
	return mustBundle(tb).serviceCACertPath
}

func ServiceE2E(tb testing.TB, certName, serviceName string) config.ServiceE2EConfig {
	tb.Helper()
	b := mustBundle(tb)
	e2e, err := b.serviceE2E(certName, serviceName)
	if err != nil {
		tb.Fatalf("generate service test certificate %q for %q: %v", certName, serviceName, err)
	}
	return e2e
}

func mustBundle(tb testing.TB) *bundle {
	tb.Helper()
	b, err := bundleOnce()
	if err != nil {
		tb.Fatalf("generate test certificates: %v", err)
	}
	return b
}

func mustBundlePanic() *bundle {
	b, err := bundleOnce()
	if err != nil {
		panic(err)
	}
	return b
}

func bundleOnce() (*bundle, error) {
	once.Do(func() {
		shared, onceErr = newBundle()
	})
	return shared, onceErr
}

func newBundle() (*bundle, error) {
	dir, err := os.MkdirTemp("", "qoru-test-certs-*")
	if err != nil {
		return nil, err
	}

	caCert, caKey, caPEM, err := newCA("qoru-test-ca")
	if err != nil {
		return nil, err
	}
	caCertPath := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caCertPath, caPEM, 0o600); err != nil {
		return nil, err
	}

	serviceCACert, serviceCAKey, serviceCAPEM, err := newCA("qoru-test-service-ca")
	if err != nil {
		return nil, err
	}
	serviceCACertPath := filepath.Join(dir, "service-ca.crt")
	if err := os.WriteFile(serviceCACertPath, serviceCAPEM, 0o600); err != nil {
		return nil, err
	}

	return &bundle{
		dir:               dir,
		caCert:            caCert,
		caKey:             caKey,
		caCertPath:        caCertPath,
		serviceCACert:     serviceCACert,
		serviceCAKey:      serviceCAKey,
		serviceCACertPath: serviceCACertPath,
		nodes:             make(map[string]config.IdentityConfig),
		service:           make(map[string]config.ServiceE2EConfig),
	}, nil
}

func (b *bundle) nodeIdentity(nodeID string) (config.IdentityConfig, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if identity, ok := b.nodes[nodeID]; ok {
		return identity, nil
	}

	certPath := filepath.Join(b.dir, nodeID+".crt")
	keyPath := filepath.Join(b.dir, nodeID+".key")
	if err := writeLeaf(certPath, keyPath, b.caCert, b.caKey, nodeID, "spiffe://qoru/node/"+nodeID, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}); err != nil {
		return config.IdentityConfig{}, err
	}
	identity := config.IdentityConfig{Cert: certPath, Key: keyPath, CA: b.caCertPath}
	b.nodes[nodeID] = identity
	return identity, nil
}

func (b *bundle) serviceE2E(certName, serviceName string) (config.ServiceE2EConfig, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := certName + "\x00" + serviceName
	if e2e, ok := b.service[key]; ok {
		return e2e, nil
	}

	certPath := filepath.Join(b.dir, certName+".crt")
	keyPath := filepath.Join(b.dir, certName+".key")
	if err := writeLeaf(certPath, keyPath, b.serviceCACert, b.serviceCAKey, certName, "spiffe://qoru/service/"+serviceName, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}); err != nil {
		return config.ServiceE2EConfig{}, err
	}
	e2e := config.ServiceE2EConfig{Cert: certPath, Key: keyPath}
	b.service[key] = e2e
	return e2e, nil
}

func newCA(commonName string) (*x509.Certificate, *ecdsa.PrivateKey, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serialNumber(),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, err
	}
	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

func writeLeaf(certPath, keyPath string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, commonName, uriValue string, usages []x509.ExtKeyUsage) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	uri, err := url.Parse(uriValue)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serialNumber(),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           usages,
		BasicConstraintsValid: true,
		URIs:                  []*url.URL{uri},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
}

func serialNumber() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return serial
}
