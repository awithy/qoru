package server

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/testcert"
)

func TestNewE2EServerRuntimeValidatesAndCachesServiceCertificates(t *testing.T) {
	cfg := testE2EServerConfig(t)

	rt, err := newE2EServerRuntime(cfg)
	if err != nil {
		t.Fatalf("newE2EServerRuntime returned error: %v", err)
	}
	if len(rt.serviceCerts) != 1 {
		t.Fatalf("expected one cached service certificate, got %d", len(rt.serviceCerts))
	}
	if _, err := rt.serviceCertificate(cfg.Services[0]); err != nil {
		t.Fatalf("expected service certificate to be cached: %v", err)
	}
}

func TestNewE2EServerRuntimeRejectsWrongServiceIdentity(t *testing.T) {
	cfg := testE2EServerConfig(t)
	cfg.Services[0].E2E = testcert.ServiceE2E(t, "relay-b-postgres", "postgres")

	_, err := newE2EServerRuntime(cfg)
	if err == nil {
		t.Fatal("expected wrong service identity to be rejected")
	}
	if !strings.Contains(err.Error(), "service identity mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewE2EServerRuntimeRejectsWrongServiceCA(t *testing.T) {
	cfg := testE2EServerConfig(t)
	cfg.ServiceIdentity.CA = testcert.CAPath(t)

	_, err := newE2EServerRuntime(cfg)
	if err == nil {
		t.Fatal("expected wrong service CA to be rejected")
	}
	if !strings.Contains(err.Error(), "verify service certificate chain") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRejectsBadE2EServiceCertificateBeforeListening(t *testing.T) {
	cfg := testE2EServerConfig(t)
	cfg.Services[0].E2E = testcert.ServiceE2E(t, "relay-b-postgres", "postgres")
	started := make(chan string, 1)
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		errCh <- Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), WithStartedFunc(func(addr string) { started <- addr }))
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected startup error")
		}
	case addr := <-started:
		t.Fatalf("server started before rejecting bad E2E cert at %s", addr)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for startup error")
	}
}

func testE2EServerConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		NodeID:          "relay-b",
		Mode:            config.ModeServer,
		Identity:        testcert.NodeIdentity(t, "relay-b"),
		ServiceIdentity: config.ServiceIdentityConfig{CA: testcert.ServiceCAPath(t)},
		Listen:          "127.0.0.1:0",
		Services: []config.ServiceConfig{{
			Name:     "echo",
			Protocol: "tcp",
			Target:   "127.0.0.1:9000",
			E2E:      testcert.ServiceE2E(t, "relay-b-echo", "echo"),
		}},
	}
}
