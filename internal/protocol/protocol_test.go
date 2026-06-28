package protocol

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestConnectTCPRequestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := ConnectTCPRequest{Target: "127.0.0.1:5432"}

	if err := WriteConnectTCPRequest(&buf, want); err != nil {
		t.Fatalf("WriteConnectTCPRequest returned error: %v", err)
	}

	got, err := ReadConnectTCPRequest(&buf)
	if err != nil {
		t.Fatalf("ReadConnectTCPRequest returned error: %v", err)
	}
	if got != want {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestWriteConnectTCPRequestRejectsEmptyTarget(t *testing.T) {
	if err := WriteConnectTCPRequest(io.Discard, ConnectTCPRequest{}); err == nil {
		t.Fatal("expected empty target to be rejected")
	}
}

func TestWriteConnectTCPRequestRejectsTooLongTarget(t *testing.T) {
	target := strings.Repeat("a", MaxTargetLength+1)
	if err := WriteConnectTCPRequest(io.Discard, ConnectTCPRequest{Target: target}); err == nil {
		t.Fatal("expected too-long target to be rejected")
	}
}

func TestReadConnectTCPRequestRejectsUnsupportedVersion(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{Version + 1, byte(TypeConnectTCP), 0, 2, 0, 0})

	_, err := ReadConnectTCPRequest(&buf)
	if err == nil {
		t.Fatal("expected unsupported version error")
	}
}

func TestReadConnectTCPRequestRejectsWrongType(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, MessageType(99), []byte{0, 0}); err != nil {
		t.Fatal(err)
	}

	_, err := ReadConnectTCPRequest(&buf)
	if err == nil {
		t.Fatal("expected wrong type error")
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	err := WriteFrame(io.Discard, TypeConnectTCP, make([]byte, MaxPayloadSize+1))
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
	_, err := ReadFrame(bytes.NewReader([]byte{Version, byte(TypeConnectTCP), 0, 4, 1}))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected unexpected EOF, got %v", err)
	}
}

func TestReadConnectTCPRequestRejectsMalformedPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypeConnectTCP, []byte{0, 4, 'a'}); err != nil {
		t.Fatal(err)
	}

	_, err := ReadConnectTCPRequest(&buf)
	if err == nil {
		t.Fatal("expected malformed payload error")
	}
}
