package e2e

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/awithy/qoru/internal/protocol"
)

func TestEncryptedRecordRoundTrip(t *testing.T) {
	key := testRecordKey(1)
	transcript := testTranscriptHash(2)
	var wire bytes.Buffer
	writer, err := NewEncryptedWriter(&wire, key, transcript)
	if err != nil {
		t.Fatalf("NewEncryptedWriter: %v", err)
	}
	plaintext := bytes.Repeat([]byte("qoru-secret-payload-"), writer.maxPlaintext/len("qoru-secret-payload-")+3)
	if _, err := writer.Write(plaintext); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := writer.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	if bytes.Contains(wire.Bytes(), []byte("qoru-secret-payload")) {
		t.Fatal("wire data contains plaintext")
	}

	reader, err := NewEncryptedReader(bytes.NewReader(wire.Bytes()), key, transcript)
	if err != nil {
		t.Fatalf("NewEncryptedReader: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch: got %d bytes want %d", len(got), len(plaintext))
	}
}

func TestEncryptedReaderReturnsEOFOnClose(t *testing.T) {
	key := testRecordKey(1)
	transcript := testTranscriptHash(2)
	var wire bytes.Buffer
	writer, err := NewEncryptedWriter(&wire, key, transcript)
	if err != nil {
		t.Fatalf("NewEncryptedWriter: %v", err)
	}
	if err := writer.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	reader, err := NewEncryptedReader(bytes.NewReader(wire.Bytes()), key, transcript)
	if err != nil {
		t.Fatalf("NewEncryptedReader: %v", err)
	}
	buf := make([]byte, 1)
	if n, err := reader.Read(buf); n != 0 || err != io.EOF {
		t.Fatalf("expected EOF, got n=%d err=%v", n, err)
	}
}

func TestEncryptedReaderRejectsTamperedCiphertext(t *testing.T) {
	key := testRecordKey(1)
	transcript := testTranscriptHash(2)
	var wire bytes.Buffer
	writer, err := NewEncryptedWriter(&wire, key, transcript)
	if err != nil {
		t.Fatalf("NewEncryptedWriter: %v", err)
	}
	if _, err := writer.Write([]byte("secret")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	tampered := append([]byte(nil), wire.Bytes()...)
	tampered[len(tampered)-1] ^= 0x01

	reader, err := NewEncryptedReader(bytes.NewReader(tampered), key, transcript)
	if err != nil {
		t.Fatalf("NewEncryptedReader: %v", err)
	}
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("expected tampered ciphertext to be rejected")
	}
}

func TestEncryptedReaderRejectsWrongTranscript(t *testing.T) {
	key := testRecordKey(1)
	var wire bytes.Buffer
	writer, err := NewEncryptedWriter(&wire, key, testTranscriptHash(2))
	if err != nil {
		t.Fatalf("NewEncryptedWriter: %v", err)
	}
	if _, err := writer.Write([]byte("secret")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	reader, err := NewEncryptedReader(bytes.NewReader(wire.Bytes()), key, testTranscriptHash(3))
	if err != nil {
		t.Fatalf("NewEncryptedReader: %v", err)
	}
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("expected wrong transcript hash to be rejected")
	}
}

func TestEncryptedReaderRejectsOutOfOrderRecord(t *testing.T) {
	key := testRecordKey(1)
	transcript := testTranscriptHash(2)
	var wire bytes.Buffer
	writer, err := NewEncryptedWriter(&wire, key, transcript)
	if err != nil {
		t.Fatalf("NewEncryptedWriter: %v", err)
	}
	if _, err := writer.Write([]byte("first")); err != nil {
		t.Fatalf("Write first: %v", err)
	}
	if _, err := writer.Write([]byte("second")); err != nil {
		t.Fatalf("Write second: %v", err)
	}

	source := bytes.NewReader(wire.Bytes())
	first, err := protocol.ReadFrame(source)
	if err != nil {
		t.Fatalf("ReadFrame first: %v", err)
	}
	second, err := protocol.ReadFrame(source)
	if err != nil {
		t.Fatalf("ReadFrame second: %v", err)
	}
	var reordered bytes.Buffer
	if err := protocol.WriteFrame(&reordered, second.Type, second.Payload); err != nil {
		t.Fatalf("WriteFrame second: %v", err)
	}
	if err := protocol.WriteFrame(&reordered, first.Type, first.Payload); err != nil {
		t.Fatalf("WriteFrame first: %v", err)
	}

	reader, err := NewEncryptedReader(bytes.NewReader(reordered.Bytes()), key, transcript)
	if err != nil {
		t.Fatalf("NewEncryptedReader: %v", err)
	}
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("expected out-of-order record to be rejected")
	}
}

func TestEncryptedWriterRejectsWriteAfterClose(t *testing.T) {
	writer, err := NewEncryptedWriter(io.Discard, testRecordKey(1), testTranscriptHash(2))
	if err != nil {
		t.Fatalf("NewEncryptedWriter: %v", err)
	}
	if err := writer.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	if _, err := writer.Write([]byte("after-close")); !errors.Is(err, ErrEncryptedWriterClosed) {
		t.Fatalf("expected ErrEncryptedWriterClosed, got %v", err)
	}
}

func TestEncryptedReaderReturnsCloseError(t *testing.T) {
	var wire bytes.Buffer
	if err := protocol.WriteE2EClose(&wire, protocol.E2EClose{Code: CloseCodeError, ConnectCode: protocol.ConnectCodeTargetDialFailed, Message: "boom"}); err != nil {
		t.Fatalf("WriteE2EClose: %v", err)
	}
	reader, err := NewEncryptedReader(&wire, testRecordKey(1), testTranscriptHash(2))
	if err != nil {
		t.Fatalf("NewEncryptedReader: %v", err)
	}
	_, err = io.ReadAll(reader)
	var closeErr *CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("expected CloseError, got %T: %v", err, err)
	}
	if closeErr.Code != CloseCodeError || closeErr.ConnectCode != protocol.ConnectCodeTargetDialFailed || closeErr.Message != "boom" {
		t.Fatalf("unexpected close error: %#v", closeErr)
	}
}

func TestDeriveTrafficKeysWorkWithEncryptedRecords(t *testing.T) {
	keys := TrafficKeys{ClientToServer: testRecordKey(1), ServerToClient: testRecordKey(2)}
	transcript := testTranscriptHash(3)
	var wire bytes.Buffer
	writer, err := NewEncryptedWriter(&wire, keys.ClientToServer, transcript)
	if err != nil {
		t.Fatalf("NewEncryptedWriter: %v", err)
	}
	reader, err := NewEncryptedReader(&wire, keys.ClientToServer, transcript)
	if err != nil {
		t.Fatalf("NewEncryptedReader: %v", err)
	}
	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := writer.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !reflect.DeepEqual(got, []byte("hello")) {
		t.Fatalf("got %q", got)
	}
}

func testRecordKey(seed byte) []byte {
	key := make([]byte, trafficKeySize)
	for i := range key {
		key[i] = seed + byte(i)
	}
	return key
}

func testTranscriptHash(seed byte) []byte {
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = seed + byte(i)
	}
	return hash
}
