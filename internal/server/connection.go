package server

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/awithy/qoru/internal/identity"
	"github.com/awithy/qoru/internal/protocol"
	"github.com/awithy/qoru/internal/requestid"
	"github.com/quic-go/quic-go"
)

const defaultTCPDialTimeout = 10 * time.Second

func (rt *serverRuntime) handleConnection(conn *quic.Conn) {
	peerID, err := identity.PeerNodeID(conn.ConnectionState().TLS)
	if err != nil {
		rt.logger.Error("extract peer identity failed", "error", err)
		_ = conn.CloseWithError(1, "peer identity required")
		return
	}
	registeredPeer := false
	if rt.peers != nil {
		registeredPeer = rt.peers.RegisterInbound(peerID, conn)
	}
	if !registeredPeer {
		rt.logger.Info("peer connected", "peer_id", peerID)
	}
	defer rt.logger.Info("peer disconnected", "peer_id", peerID)

	var streamWG sync.WaitGroup
	for {
		stream, err := conn.AcceptStream(rt.ctx)
		if err != nil {
			if rt.ctx.Err() == nil {
				rt.logger.Error("accept stream failed", "error", err)
			}
			_ = conn.CloseWithError(0, "done")
			if waitErr := waitGroupTimeout(&streamWG, defaultShutdownWaitTimeout); waitErr != nil {
				rt.logger.Warn("timed out waiting for streams to close", "peer_id", peerID, "error", waitErr)
			}
			return
		}
		streamWG.Go(func() {
			rt.handleStream(peerID, stream)
		})
	}
}

func (rt *serverRuntime) handleStream(peerID string, stream *quic.Stream) {
	requestID, err := requestid.New()
	if err != nil {
		rt.logger.Error("generate request id failed", "peer_id", peerID, "error", err)
		_ = stream.Close()
		return
	}
	logger := rt.logger.With("request_id", requestID, "peer_id", peerID)

	req, err := protocol.ReadConnectRequest(stream)
	if err != nil {
		logger.Error("read connect request failed", "error", err)
		_ = stream.Close()
		return
	}

	logger = logger.With("protocol", req.Protocol, "service", req.Service, "egress", req.Egress, "route", req.Route)
	logger.Info("connect requested")
	if rt.opts.connectRequest != nil {
		rt.opts.connectRequest(req)
	}

	if req.Protocol != "tcp" {
		err := fmt.Errorf("unsupported connect protocol %q", req.Protocol)
		logger.Warn("connect protocol unsupported", "response_code", protocol.ConnectCodeUnsupportedProtocol.String(), "error", err)
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeUnsupportedProtocol, Message: err.Error()})
		_ = stream.Close()
		return
	}

	if err := validateConnectRoute(rt.cfg.NodeID, req.Route, req.Egress); err != nil {
		logger.Warn("route invalid", "response_code", protocol.ConnectCodeRouteInvalid.String(), "error", err)
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeRouteInvalid, Message: err.Error()})
		_ = stream.Close()
		return
	}

	if len(req.Route) > 1 {
		rt.handleRelayStream(logger, req, stream)
		return
	}

	if req.Egress != "" && req.Egress != rt.cfg.NodeID {
		err := fmt.Errorf("egress %q is not reachable from one-hop server %q", req.Egress, rt.cfg.NodeID)
		logger.Warn("egress unsupported", "response_code", protocol.ConnectCodeUnreachableEgress.String(), "error", err)
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeUnreachableEgress, Message: err.Error()})
		_ = stream.Close()
		return
	}

	svc, err := resolveService(rt.cfg, peerID, req.Protocol, req.Service)
	if err != nil {
		code := serviceErrorCode(err)
		logger.Warn("service denied", "response_code", code.String(), "error", err)
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: code, Message: err.Error()})
		_ = stream.Close()
		return
	}

	targetConn, err := dialTCP(rt.ctx, svc.Target)
	if err != nil {
		logger.Error("tcp target dial failed", "target", svc.Target, "response_code", protocol.ConnectCodeTargetDialFailed.String(), "error", err)
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeTargetDialFailed, Message: err.Error()})
		_ = stream.Close()
		return
	}
	defer targetConn.Close()

	if err := protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: true}); err != nil {
		logger.Error("write connect tcp response failed", "target", svc.Target, "error", err)
		_ = stream.Close()
		return
	}

	logger.Info("tcp target connected", "target", svc.Target)

	proxyTCP(stream, targetConn)

	logger.Info("tcp proxy closed", "target", svc.Target)
}

func (rt *serverRuntime) handleRelayStream(logger *slog.Logger, req protocol.ConnectRequest, inbound *quic.Stream) {
	nextHop := req.Route[1]
	logger = logger.With("next_hop", nextHop)
	if rt.peers == nil {
		err := fmt.Errorf("peer sessions are not initialized")
		logger.Error("peer sessions unavailable", "response_code", protocol.ConnectCodeInternalError.String(), "error", err)
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeInternalError, Message: err.Error()})
		_ = inbound.Close()
		return
	}

	outbound, err := rt.peers.OpenStream(rt.ctx, nextHop)
	if err != nil {
		logger.Warn("next hop unreachable", "response_code", protocol.ConnectCodeNextHopUnreachable.String(), "error", err)
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeNextHopUnreachable, Message: err.Error()})
		_ = inbound.Close()
		return
	}
	defer outbound.Close()

	forwarded := req
	forwarded.Route = append([]string(nil), req.Route[1:]...)
	if err := protocol.WriteConnectRequest(outbound, forwarded); err != nil {
		logger.Warn("write next hop connect request failed", "response_code", protocol.ConnectCodeNextHopUnreachable.String(), "error", err)
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeNextHopUnreachable, Message: err.Error()})
		_ = inbound.Close()
		return
	}
	resp, err := protocol.ReadConnectResponse(outbound)
	if err != nil {
		logger.Warn("read next hop connect response failed", "response_code", protocol.ConnectCodeNextHopUnreachable.String(), "error", err)
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeNextHopUnreachable, Message: err.Error()})
		_ = inbound.Close()
		return
	}
	if err := protocol.WriteConnectResponse(inbound, resp); err != nil {
		logger.Warn("write upstream connect response failed", "response_code", resp.Code.String(), "error", err)
		_ = inbound.Close()
		return
	}
	if !resp.OK {
		logger.Warn("relay connect rejected", "response_code", resp.Code.String(), "error", resp.Message)
		_ = inbound.Close()
		return
	}
	logger.Info("relay stream connected", "response_code", resp.Code.String())
	proxyStreams(inbound, outbound)
	logger.Info("relay proxy closed")
}

func validateConnectRoute(nodeID string, route []string, egress string) error {
	if len(route) == 0 {
		return nil
	}
	if route[0] != nodeID {
		return fmt.Errorf("route first hop %q does not match this node %q", route[0], nodeID)
	}
	if egress != "" && egress != route[len(route)-1] {
		return fmt.Errorf("egress %q must match final route hop %q", egress, route[len(route)-1])
	}
	return nil
}

func serviceErrorCode(err error) protocol.ConnectCode {
	switch {
	case errors.Is(err, ErrServiceNotFound):
		return protocol.ConnectCodeServiceNotFound
	case errors.Is(err, ErrAccessDenied):
		return protocol.ConnectCodeAccessDenied
	default:
		return protocol.ConnectCodeInternalError
	}
}
