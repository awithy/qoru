package client

import (
	"net"

	"github.com/awithy/qoru/internal/e2e"
	"github.com/awithy/qoru/internal/proxyio"
	"github.com/quic-go/quic-go"
)

func proxyTCP(localConn net.Conn, stream *quic.Stream) error {
	return proxyio.Proxy(proxyio.NetConnEndpoint(localConn), proxyio.QUICStreamEndpoint(stream))
}

func proxyEncryptedTCP(localConn net.Conn, stream *quic.Stream, reader *e2e.EncryptedReader, writer *e2e.EncryptedWriter) error {
	return proxyio.Proxy(proxyio.NetConnEndpoint(localConn), proxyio.Endpoint{
		Reader:     reader,
		Writer:     writer,
		CloseWrite: writer.CloseWrite,
		Abort: func() {
			stream.CancelRead(0)
			stream.CancelWrite(0)
			_ = stream.Close()
		},
		Close: stream.Close,
	})
}
