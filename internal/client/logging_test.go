package client

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
	err := e2ePhaseError("read_server_hello", base)

	if got := e2eErrorPhase(err); got != "read_server_hello" {
		t.Fatalf("expected phase read_server_hello, got %q", got)
	}
	if !errors.Is(err, base) {
		t.Fatal("expected phase error to unwrap base error")
	}
}

func TestE2ESetupErrorMapsWrappedCloseConnectCode(t *testing.T) {
	err := e2ePhaseError("read_server_hello", &e2e.CloseError{Code: e2e.CloseCodeError, ConnectCode: protocol.ConnectCodeTargetDialFailed, Message: "dial failed"})

	setupErr := e2eSetupError(err)
	var rejected *ConnectRejectedError
	if !errors.As(setupErr, &rejected) {
		t.Fatalf("expected ConnectRejectedError, got %T: %v", setupErr, setupErr)
	}
	if rejected.Code != protocol.ConnectCodeTargetDialFailed || rejected.Message != "dial failed" {
		t.Fatalf("unexpected rejection: %#v", rejected)
	}
}

func TestLogE2EHandshakeFailedIncludesPhaseAndCloseFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	err := e2ePhaseError("read_server_hello", &e2e.CloseError{Code: e2e.CloseCodeError, ConnectCode: protocol.ConnectCodeAccessDenied, Message: "denied"})

	logE2EHandshakeFailed(logger, err)

	out := buf.String()
	for _, want := range []string{
		"msg=\"e2e handshake failed\"",
		"e2e_phase=read_server_hello",
		"response_code=ACCESS_DENIED",
		"close_code=1",
		"close_message=denied",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, out)
		}
	}
}

func TestLogE2EProxyErrorIncludesCloseFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	err := &e2e.CloseError{Code: e2e.CloseCodeError, ConnectCode: protocol.ConnectCodeInternalError, Message: "boom"}

	logE2EProxyError(logger, err)

	out := buf.String()
	for _, want := range []string{
		"msg=\"e2e proxy closed with error\"",
		"response_code=INTERNAL_ERROR",
		"close_code=1",
		"close_message=boom",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, out)
		}
	}
}
