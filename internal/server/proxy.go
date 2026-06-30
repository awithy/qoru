package server

import (
	"io"
	"net"

	"github.com/quic-go/quic-go"
)

func proxyStreams(a, b *quic.Stream) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(a, b)
		_ = a.Close()
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(b, a)
		_ = b.Close()
		done <- struct{}{}
	}()
	<-done
	_ = a.Close()
	_ = b.Close()
}

func proxyTCP(stream *quic.Stream, targetConn net.Conn) {
	done := make(chan struct{}, 2)

	go func() {
		_, _ = io.Copy(targetConn, stream)
		if tcpConn, ok := targetConn.(*net.TCPConn); ok {
			_ = tcpConn.CloseWrite()
		} else {
			_ = targetConn.Close()
		}
		done <- struct{}{}
	}()

	go func() {
		_, _ = io.Copy(stream, targetConn)
		_ = stream.Close()
		done <- struct{}{}
	}()

	<-done
	_ = targetConn.Close()
	_ = stream.Close()
}
