package server

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/awithy/qoru/internal/e2e"
	"github.com/awithy/qoru/internal/protocol"
)

func TestE2EErrorPhaseUnwrapsPhaseError(t *testing.T) {
	base := errors.New("boom")
	err := e2ePhaseError("authorize_client", base)

	if got := e2eErrorPhase(err); got != "authorize_client" {
		t.Fatalf("expected phase authorize_client, got %q", got)
	}
	if !errors.Is(err, base) {
		t.Fatal("expected phase error to unwrap base error")
	}
}

func TestE2EConnectErrorCodeUnwrapsPhaseError(t *testing.T) {
	err := e2ePhaseError("prepare_service", &e2eConnectError{code: protocol.ConnectCodeTargetDialFailed, err: errors.New("dial failed")})

	if got := e2eConnectErrorCode(err); got != protocol.ConnectCodeTargetDialFailed {
		t.Fatalf("expected TARGET_DIAL_FAILED, got %s", got)
	}
}

func TestLogE2EProxyErrorIncludesCloseFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	err := &e2e.CloseError{Code: e2e.CloseCodeError, ConnectCode: protocol.ConnectCodeAccessDenied, Message: "denied"}

	logE2EProxyError(logger, "server", err, "target", "127.0.0.1:9000", "original_client_id", "client-1")

	out := buf.String()
	for _, want := range []string{
		"msg=\"e2e proxy closed with error\"",
		"direction=server",
		"response_code=ACCESS_DENIED",
		"close_code=1",
		"close_message=denied",
		"target=127.0.0.1:9000",
		"original_client_id=client-1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, out)
		}
	}
}

func TestRequestLoggerIncludesRequestFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	rt := &serverRuntime{logger: logger}
	req := protocol.ConnectRequest{
		RequestID: "018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a0001",
		Protocol:  "tcp",
		Service:   "echo",
		Egress:    "relay-b",
		Route:     []string{"relay-a", "relay-b"},
	}

	reqLogger := rt.requestLogger("client-1", req)
	reqLogger.Info("connect requested", "response_code", protocol.ConnectCodeOK.String())

	out := buf.String()
	for _, want := range []string{
		"msg=\"connect requested\"",
		"peer_id=client-1",
		"request_id=018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a0001",
		"protocol=tcp",
		"service=echo",
		"egress=relay-b",
		"route=",
		"response_code=OK",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, out)
		}
	}
}
