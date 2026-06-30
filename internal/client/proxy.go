package client

import (
	"net"

	"github.com/awithy/qoru/internal/proxyio"
	"github.com/quic-go/quic-go"
)

func proxyTCP(localConn net.Conn, stream *quic.Stream) {
	proxyio.Proxy(proxyio.NetConnEndpoint(localConn), proxyio.QUICStreamEndpoint(stream))
}
