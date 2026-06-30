package server

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/awithy/qoru/internal/protocol"
)

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
