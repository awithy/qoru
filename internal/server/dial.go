package server

import (
	"context"
	"fmt"
	"net"
)

func dialTCP(ctx context.Context, target string) (net.Conn, error) {
	if _, _, err := net.SplitHostPort(target); err != nil {
		return nil, fmt.Errorf("invalid target address %q: %w", target, err)
	}

	dialCtx, cancel := context.WithTimeout(ctx, defaultTCPDialTimeout)
	defer cancel()

	dialer := net.Dialer{}
	return dialer.DialContext(dialCtx, "tcp", target)
}
