package server

import (
	"net"

	"github.com/awithy/qoru/internal/e2e"
	"github.com/awithy/qoru/internal/proxyio"
	"github.com/quic-go/quic-go"
)

func proxyStreams(a, b *quic.Stream) error {
	return proxyio.Proxy(proxyio.QUICStreamEndpoint(a), proxyio.QUICStreamEndpoint(b))
}

func proxyTCP(stream *quic.Stream, targetConn net.Conn) error {
	return proxyio.Proxy(proxyio.QUICStreamEndpoint(stream), proxyio.NetConnEndpoint(targetConn))
}

func proxyEncryptedTCP(stream *quic.Stream, reader *e2e.EncryptedReader, writer *e2e.EncryptedWriter, targetConn net.Conn) error {
	return proxyio.Proxy(proxyio.Endpoint{
		Reader:     reader,
		Writer:     writer,
		CloseWrite: writer.CloseWrite,
		Abort: func() {
			stream.CancelRead(0)
			stream.CancelWrite(0)
			_ = stream.Close()
		},
		Close: stream.Close,
	}, proxyio.NetConnEndpoint(targetConn))
}
