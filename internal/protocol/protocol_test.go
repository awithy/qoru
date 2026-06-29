package protocol

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestConnectRequestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := ConnectRequest{Protocol: "tcp", Service: "echo", Egress: "server-1"}

	if err := WriteConnectRequest(&buf, want); err != nil {
		t.Fatalf("WriteConnectRequest returned error: %v", err)
	}

	got, err := ReadConnectRequest(&buf)
	if err != nil {
		t.Fatalf("ReadConnectRequest returned error: %v", err)
	}
	if got != want {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestConnectResponseRoundTripOK(t *testing.T) {
	var buf bytes.Buffer
	want := ConnectResponse{OK: true}

	if err := WriteConnectResponse(&buf, want); err != nil {
		t.Fatalf("WriteConnectResponse returned error: %v", err)
	}

	got, err := ReadConnectResponse(&buf)
	if err != nil {
		t.Fatalf("ReadConnectResponse returned error: %v", err)
	}
	if got != want {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestConnectResponseRoundTripError(t *testing.T) {
	var buf bytes.Buffer
	want := ConnectResponse{OK: false, Code: ConnectCodeTargetDialFailed, Message: "dial failed"}

	if err := WriteConnectResponse(&buf, want); err != nil {
		t.Fatalf("WriteConnectResponse returned error: %v", err)
	}

	got, err := ReadConnectResponse(&buf)
	if err != nil {
		t.Fatalf("ReadConnectResponse returned error: %v", err)
	}
	if got != want {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestWriteConnectRequestRejectsEmptyProtocol(t *testing.T) {
	if err := WriteConnectRequest(io.Discard, ConnectRequest{Service: "echo"}); err == nil {
		t.Fatal("expected empty protocol to be rejected")
	}
}

func TestWriteConnectRequestRejectsEmptyService(t *testing.T) {
	if err := WriteConnectRequest(io.Discard, ConnectRequest{Protocol: "tcp"}); err == nil {
		t.Fatal("expected empty service to be rejected")
	}
}

func TestWriteConnectRequestRejectsTooLongService(t *testing.T) {
	service := strings.Repeat("a", MaxTargetLength+1)
	if err := WriteConnectRequest(io.Discard, ConnectRequest{Protocol: "tcp", Service: service}); err == nil {
		t.Fatal("expected too-long service to be rejected")
	}
}

func TestReadConnectRequestRejectsUnsupportedVersion(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{Version + 1, byte(TypeConnectRequest), 0, 3, 1, 'x', 0})

	_, err := ReadConnectRequest(&buf)
	if err == nil {
		t.Fatal("expected unsupported version error")
	}
}

func TestReadConnectRequestRejectsWrongType(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, MessageType(99), []byte{1, 'x', 0, 1, 'y'}); err != nil {
		t.Fatal(err)
	}

	_, err := ReadConnectRequest(&buf)
	if err == nil {
		t.Fatal("expected wrong type error")
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	err := WriteFrame(io.Discard, TypeConnectRequest, make([]byte, MaxPayloadSize+1))
	if err == nil {
		t.Fatal("expected oversized payload error")
	}
}

func TestReadFrameRejectsTruncatedHeader(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader([]byte{Version}))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected unexpected EOF, got %v", err)
	}
}

func TestReadFrameRejectsTruncatedPayload(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader([]byte{Version, byte(TypeConnectRequest), 0, 4, 1}))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected unexpected EOF, got %v", err)
	}
}

func TestReadConnectRequestRejectsMalformedPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypeConnectRequest, []byte{3, 't', 'c', 'p', 0, 4, 'a'}); err != nil {
		t.Fatal(err)
	}

	_, err := ReadConnectRequest(&buf)
	if err == nil {
		t.Fatal("expected malformed payload error")
	}
}

func TestReadConnectResponseRejectsMalformedPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypeConnectResponse, []byte{ConnectStatusError, byte(ConnectCodeInternalError), 0, 4, 'n'}); err != nil {
		t.Fatal(err)
	}

	_, err := ReadConnectResponse(&buf)
	if err == nil {
		t.Fatal("expected malformed payload error")
	}
}

func TestReadConnectResponseRejectsInvalidCode(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypeConnectResponse, []byte{ConnectStatusError, 99, 0, 0}); err != nil {
		t.Fatal(err)
	}

	_, err := ReadConnectResponse(&buf)
	if err == nil {
		t.Fatal("expected invalid code error")
	}
}
