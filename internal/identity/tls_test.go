package identity

import (
	"crypto/tls"
	"testing"

	"github.com/awithy/qoru/internal/config"
)

func TestServerTLSConfig(t *testing.T) {
	cfg := devIdentity(t, "server-1")

	tlsConfig, err := ServerTLSConfig(cfg)
	if err != nil {
		t.Fatalf("ServerTLSConfig returned error: %v", err)
	}

	if tlsConfig.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected TLS 1.3 min version, got %d", tlsConfig.MinVersion)
	}
	if tlsConfig.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("expected RequireAndVerifyClientCert, got %v", tlsConfig.ClientAuth)
	}
	if tlsConfig.ClientCAs == nil {
		t.Fatal("expected ClientCAs to be set")
	}
	if len(tlsConfig.Certificates) != 1 {
		t.Fatalf("expected one certificate, got %d", len(tlsConfig.Certificates))
	}
	if len(tlsConfig.NextProtos) != 1 || tlsConfig.NextProtos[0] != ALPN {
		t.Fatalf("expected ALPN %q, got %#v", ALPN, tlsConfig.NextProtos)
	}
}

func TestClientTLSConfig(t *testing.T) {
	cfg := devIdentity(t, "client-1")

	tlsConfig, err := ClientTLSConfig(cfg, "server-1")
	if err != nil {
		t.Fatalf("ClientTLSConfig returned error: %v", err)
	}

	if tlsConfig.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected TLS 1.3 min version, got %d", tlsConfig.MinVersion)
	}
	if tlsConfig.RootCAs == nil {
		t.Fatal("expected RootCAs to be set")
	}
	if tlsConfig.ServerName != "server-1" {
		t.Fatalf("expected ServerName server-1, got %q", tlsConfig.ServerName)
	}
	if len(tlsConfig.Certificates) != 1 {
		t.Fatalf("expected one certificate, got %d", len(tlsConfig.Certificates))
	}
	if len(tlsConfig.NextProtos) != 1 || tlsConfig.NextProtos[0] != ALPN {
		t.Fatalf("expected ALPN %q, got %#v", ALPN, tlsConfig.NextProtos)
	}
}

func TestTLSConfigMissingFilesReturnError(t *testing.T) {
	_, err := ServerTLSConfig(config.IdentityConfig{Cert: "missing.crt", Key: "missing.key", CA: "missing-ca.crt"})
	if err == nil {
		t.Fatal("expected missing files to return an error")
	}
}

func devIdentity(t *testing.T, node string) config.IdentityConfig {
	t.Helper()
	return config.IdentityConfig{
		Cert: "../../dev/certs/" + node + ".crt",
		Key:  "../../dev/certs/" + node + ".key",
		CA:   "../../dev/certs/ca.crt",
	}
}
