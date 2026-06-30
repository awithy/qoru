package server

import (
	"net"

	"github.com/awithy/qoru/internal/proxyio"
	"github.com/quic-go/quic-go"
)

func proxyStreams(a, b *quic.Stream) {
	proxyio.Proxy(proxyio.QUICStreamEndpoint(a), proxyio.QUICStreamEndpoint(b))
}

func proxyTCP(stream *quic.Stream, targetConn net.Conn) {
	proxyio.Proxy(proxyio.QUICStreamEndpoint(stream), proxyio.NetConnEndpoint(targetConn))
}
