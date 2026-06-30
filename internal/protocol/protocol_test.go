package protocol

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

const testRequestID = "018ff6f2-5c7b-7d4a-b7f1-9c0e6e7a1234"

func TestConnectRequestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := ConnectRequest{RequestID: testRequestID, Protocol: "tcp", Service: "echo", Egress: "server-1", Route: []string{"server-1"}}

	if err := WriteConnectRequest(&buf, want); err != nil {
		t.Fatalf("WriteConnectRequest returned error: %v", err)
	}

	got, err := ReadConnectRequest(&buf)
	if err != nil {
		t.Fatalf("ReadConnectRequest returned error: %v", err)
	}
	if got.RequestID != want.RequestID || got.Protocol != want.Protocol || got.Service != want.Service || got.Egress != want.Egress || strings.Join(got.Route, ",") != strings.Join(want.Route, ",") {
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
	if err := WriteConnectRequest(io.Discard, ConnectRequest{RequestID: testRequestID, Service: "echo"}); err == nil {
		t.Fatal("expected empty protocol to be rejected")
	}
}

func TestWriteConnectRequestRejectsEmptyService(t *testing.T) {
	if err := WriteConnectRequest(io.Discard, ConnectRequest{RequestID: testRequestID, Protocol: "tcp"}); err == nil {
		t.Fatal("expected empty service to be rejected")
	}
}

func TestWriteConnectRequestRejectsTooLongService(t *testing.T) {
	service := strings.Repeat("a", MaxTargetLength+1)
	if err := WriteConnectRequest(io.Discard, ConnectRequest{RequestID: testRequestID, Protocol: "tcp", Service: service}); err == nil {
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

func TestE2EClientHelloRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := E2EClientHello{ClientCertChain: [][]byte{[]byte("client-cert"), []byte("ca-cert")}, EphemeralPublicKey: []byte("client-key"), Signature: []byte("client-sig")}

	if err := WriteE2EClientHello(&buf, want); err != nil {
		t.Fatalf("WriteE2EClientHello returned error: %v", err)
	}
	got, err := ReadE2EClientHello(&buf)
	if err != nil {
		t.Fatalf("ReadE2EClientHello returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestE2EServerHelloRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := E2EServerHello{ServiceCertChain: [][]byte{[]byte("service-cert")}, EphemeralPublicKey: []byte("server-key"), Signature: []byte("server-sig")}

	if err := WriteE2EServerHello(&buf, want); err != nil {
		t.Fatalf("WriteE2EServerHello returned error: %v", err)
	}
	got, err := ReadE2EServerHello(&buf)
	if err != nil {
		t.Fatalf("ReadE2EServerHello returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestE2EDataRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := E2EData{NonceSuffix: []byte("nonce"), Ciphertext: []byte("ciphertext")}

	if err := WriteE2EData(&buf, want); err != nil {
		t.Fatalf("WriteE2EData returned error: %v", err)
	}
	got, err := ReadE2EData(&buf)
	if err != nil {
		t.Fatalf("ReadE2EData returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestE2ECloseRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := E2EClose{Code: 7, Message: "closed"}

	if err := WriteE2EClose(&buf, want); err != nil {
		t.Fatalf("WriteE2EClose returned error: %v", err)
	}
	got, err := ReadE2EClose(&buf)
	if err != nil {
		t.Fatalf("ReadE2EClose returned error: %v", err)
	}
	if got != want {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestWriteE2EClientHelloRejectsMissingFields(t *testing.T) {
	valid := E2EClientHello{ClientCertChain: [][]byte{[]byte("cert")}, EphemeralPublicKey: []byte("key"), Signature: []byte("sig")}
	cases := []struct {
		name  string
		hello E2EClientHello
	}{
		{name: "cert chain", hello: E2EClientHello{EphemeralPublicKey: valid.EphemeralPublicKey, Signature: valid.Signature}},
		{name: "ephemeral key", hello: E2EClientHello{ClientCertChain: valid.ClientCertChain, Signature: valid.Signature}},
		{name: "signature", hello: E2EClientHello{ClientCertChain: valid.ClientCertChain, EphemeralPublicKey: valid.EphemeralPublicKey}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := WriteE2EClientHello(io.Discard, tc.hello); err == nil {
				t.Fatal("expected missing field to be rejected")
			}
		})
	}
}

func TestWriteE2EClientHelloRejectsOversizedCertChain(t *testing.T) {
	chain := make([][]byte, MaxE2ECertChainCount+1)
	for i := range chain {
		chain[i] = []byte("cert")
	}
	err := WriteE2EClientHello(io.Discard, E2EClientHello{ClientCertChain: chain, EphemeralPublicKey: []byte("key"), Signature: []byte("sig")})
	if err == nil {
		t.Fatal("expected oversized cert chain to be rejected")
	}
}

func TestWriteE2EDataRejectsMissingFields(t *testing.T) {
	if err := WriteE2EData(io.Discard, E2EData{Ciphertext: []byte("ciphertext")}); err == nil {
		t.Fatal("expected missing nonce to be rejected")
	}
	if err := WriteE2EData(io.Discard, E2EData{NonceSuffix: []byte("nonce")}); err == nil {
		t.Fatal("expected missing ciphertext to be rejected")
	}
}

func TestReadE2EClientHelloRejectsWrongType(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypeE2EServerHello, []byte{0}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadE2EClientHello(&buf); err == nil {
		t.Fatal("expected wrong type to be rejected")
	}
}

func TestReadE2EClientHelloRejectsMalformedPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypeE2EClientHello, []byte{1, 0, 4, 'c'}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadE2EClientHello(&buf); err == nil {
		t.Fatal("expected malformed hello payload to be rejected")
	}
}

func TestReadE2EDataRejectsMalformedPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypeE2EData, []byte{5, 'n'}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadE2EData(&buf); err == nil {
		t.Fatal("expected malformed data payload to be rejected")
	}
}

func TestReadE2ECloseRejectsMalformedPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, TypeE2EClose, []byte{1, 0, 4, 'n'}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadE2EClose(&buf); err == nil {
		t.Fatal("expected malformed close payload to be rejected")
	}
}
