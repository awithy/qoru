package client

import (
	"io"
	"net"

	"github.com/quic-go/quic-go"
)

func proxyTCP(localConn net.Conn, stream *quic.Stream) {
	done := make(chan struct{}, 2)

	go func() {
		_, _ = io.Copy(stream, localConn)
		_ = stream.Close()
		done <- struct{}{}
	}()

	go func() {
		_, _ = io.Copy(localConn, stream)
		if tcpConn, ok := localConn.(*net.TCPConn); ok {
			_ = tcpConn.CloseWrite()
		} else {
			_ = localConn.Close()
		}
		done <- struct{}{}
	}()

	<-done
	_ = localConn.Close()
	_ = stream.Close()
}
