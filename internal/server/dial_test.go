package server

import (
	"context"
	"strings"
	"testing"
)

func TestDialTCPRejectsInvalidTargetAddress(t *testing.T) {
	_, err := dialTCP(context.Background(), "localhost")
	if err == nil {
		t.Fatal("expected invalid target address to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid target address") {
		t.Fatalf("unexpected error: %v", err)
	}
}
