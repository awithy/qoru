package proxyio

import (
	"io"
	"net"
	"sync"

	"github.com/quic-go/quic-go"
)

// Endpoint describes one bidirectional proxy endpoint.
// CloseWrite should gracefully close only the endpoint's send/write direction.
// Abort should force both reads and writes to unblock after an unexpected copy error.
// Close performs final resource cleanup once both copy directions have stopped.
type Endpoint struct {
	Reader     io.Reader
	Writer     io.Writer
	CloseWrite func() error
	Abort      func()
	Close      func() error
}

// Proxy copies bytes in both directions until both sides finish.
// A normal EOF in one direction only half-closes the opposite write side, allowing
// protocols that half-close requests before reading responses to complete.
// Unexpected copy or half-close errors abort both endpoints to unblock the other direction.
func Proxy(a, b Endpoint) {
	done := make(chan struct{}, 2)
	var abortOnce sync.Once
	abort := func() {
		abortOnce.Do(func() {
			if a.Abort != nil {
				a.Abort()
			}
			if b.Abort != nil {
				b.Abort()
			}
		})
	}

	go proxyOneWay(b, a, abort, done)
	go proxyOneWay(a, b, abort, done)

	<-done
	<-done

	if a.Close != nil {
		_ = a.Close()
	}
	if b.Close != nil {
		_ = b.Close()
	}
}

func proxyOneWay(dst, src Endpoint, abort func(), done chan<- struct{}) {
	_, copyErr := io.Copy(dst.Writer, src.Reader)
	var closeErr error
	if dst.CloseWrite != nil {
		closeErr = dst.CloseWrite()
	}
	if copyErr != nil || closeErr != nil {
		abort()
	}
	done <- struct{}{}
}

func NetConnEndpoint(conn net.Conn) Endpoint {
	return Endpoint{
		Reader:     conn,
		Writer:     conn,
		CloseWrite: closeNetConnWrite(conn),
		Abort: func() {
			_ = conn.Close()
		},
		Close: conn.Close,
	}
}

func closeNetConnWrite(conn net.Conn) func() error {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		return tcpConn.CloseWrite
	}
	return conn.Close
}

func QUICStreamEndpoint(stream *quic.Stream) Endpoint {
	return Endpoint{
		Reader:     stream,
		Writer:     stream,
		CloseWrite: stream.Close,
		Abort: func() {
			stream.CancelRead(0)
			stream.CancelWrite(0)
			_ = stream.Close()
		},
		Close: stream.Close,
	}
}
