package requestid

import "testing"

func TestNewReturnsUUIDv7(t *testing.T) {
	id, err := New()
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	bytes, err := ParseBytes(id)
	if err != nil {
		t.Fatalf("ParseBytes returned error: %v", err)
	}
	got, err := FromBytes(bytes[:])
	if err != nil {
		t.Fatalf("FromBytes returned error: %v", err)
	}
	if got != id {
		t.Fatalf("expected %q, got %q", id, got)
	}
}

func TestParseBytesRejectsNonV7UUID(t *testing.T) {
	_, err := ParseBytes("550e8400-e29b-41d4-a716-446655440000")
	if err == nil {
		t.Fatal("expected non-v7 UUID to be rejected")
	}
}
