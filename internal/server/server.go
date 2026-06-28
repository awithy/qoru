package server

import (
	"context"
	"fmt"
	"io"

	"github.com/awithy/qoru/internal/config"
	"github.com/awithy/qoru/internal/identity"
	"github.com/quic-go/quic-go"
)

type options struct {
	started func(addr string)
}

type Option func(*options)

func WithStartedFunc(fn func(addr string)) Option {
	return func(opts *options) {
		opts.started = fn
	}
}

func Run(ctx context.Context, cfg *config.Config, out io.Writer, runOptions ...Option) error {
	if err := config.ValidateServer(cfg); err != nil {
		return err
	}

	tlsConfig, err := identity.ServerTLSConfig(cfg.Identity)
	if err != nil {
		return err
	}

	listener, err := quic.ListenAddr(cfg.Listen, tlsConfig, &quic.Config{})
	if err != nil {
		return err
	}
	defer listener.Close()

	opts := options{}
	for _, apply := range runOptions {
		apply(&opts)
	}

	addr := listener.Addr().String()
	if out != nil {
		fmt.Fprintf(out, "server node %s listening on %s\n", cfg.NodeID, addr)
	}
	if opts.started != nil {
		opts.started(addr)
	}

	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		_ = conn.CloseWithError(0, "not implemented")
	}
}
