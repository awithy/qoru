package server

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/awithy/qoru/internal/identity"
	"github.com/awithy/qoru/internal/protocol"
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
				rt.logger.Error("accept stream failed", "peer_id", peerID, "error", err)
			}
			_ = conn.CloseWithError(0, "done")
			if waitErr := waitGroupTimeout(&streamWG, defaultShutdownWaitTimeout); waitErr != nil {
				rt.logger.Warn("timed out waiting for streams to close", "peer_id", peerID, "error", waitErr)
			}
			return
		}
		streamWG.Add(1)
		go func() {
			defer streamWG.Done()
			rt.handleStream(peerID, stream)
		}()
	}
}

func (rt *serverRuntime) handleStream(peerID string, stream *quic.Stream) {
	req, err := protocol.ReadConnectRequest(stream)
	if err != nil {
		rt.logger.Error("read connect request failed", "peer_id", peerID, "error", err)
		_ = stream.Close()
		return
	}

	logger := rt.requestLogger(peerID, req)
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
		if err := authorizeRelayIngress(rt.cfg, peerID); err != nil {
			logger.Warn("relay ingress denied", "response_code", protocol.ConnectCodeAccessDenied.String(), "error", err)
			_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeAccessDenied, Message: err.Error()})
			_ = stream.Close()
			return
		}
		rt.handleRelayStream(req, stream, logger)
		return
	}

	if len(req.Route) == 1 {
		if err := requireConfiguredPeer(rt.cfg, peerID); err != nil {
			logger.Warn("routed egress peer denied", "response_code", protocol.ConnectCodeAccessDenied.String(), "error", err)
			_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeAccessDenied, Message: err.Error()})
			_ = stream.Close()
			return
		}
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
		logger.Error("write connect tcp response failed", "target", svc.Target, "response_code", protocol.ConnectCodeOK.String(), "error", err)
		_ = stream.Close()
		return
	}

	logger.Info("tcp target connected", "target", svc.Target, "response_code", protocol.ConnectCodeOK.String())

	proxyTCP(stream, targetConn)

	logger.Info("tcp proxy closed", "target", svc.Target)
}

func (rt *serverRuntime) requestLogger(peerID string, req protocol.ConnectRequest) *slog.Logger {
	return rt.logger.With(
		"peer_id", peerID,
		"request_id", req.RequestID,
		"protocol", req.Protocol,
		"service", req.Service,
		"egress", req.Egress,
		"route", req.Route,
	)
}

func (rt *serverRuntime) handleRelayStream(req protocol.ConnectRequest, inbound *quic.Stream, logger *slog.Logger) {
	nextHop := req.Route[1]
	logger = logger.With("next_hop", nextHop)
	if rt.peers == nil {
		err := fmt.Errorf("peer sessions are not initialized")
		logger.Error("relay peer sessions unavailable", "response_code", protocol.ConnectCodeInternalError.String(), "error", err)
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeInternalError, Message: err.Error()})
		_ = inbound.Close()
		return
	}

	outbound, err := rt.peers.OpenStream(rt.ctx, nextHop)
	if err != nil {
		logger.Warn("relay next hop unreachable", "response_code", protocol.ConnectCodeNextHopUnreachable.String(), "error", err)
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeNextHopUnreachable, Message: err.Error()})
		_ = inbound.Close()
		return
	}
	defer outbound.Close()

	forwarded := req
	forwarded.Route = append([]string(nil), req.Route[1:]...)
	if err := protocol.WriteConnectRequest(outbound, forwarded); err != nil {
		logger.Warn("relay write request failed", "response_code", protocol.ConnectCodeNextHopUnreachable.String(), "forwarded_route", forwarded.Route, "error", err)
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeNextHopUnreachable, Message: err.Error()})
		_ = inbound.Close()
		return
	}
	resp, err := protocol.ReadConnectResponse(outbound)
	if err != nil {
		logger.Warn("relay read response failed", "response_code", protocol.ConnectCodeNextHopUnreachable.String(), "forwarded_route", forwarded.Route, "error", err)
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeNextHopUnreachable, Message: err.Error()})
		_ = inbound.Close()
		return
	}
	if err := protocol.WriteConnectResponse(inbound, resp); err != nil {
		logger.Error("relay write response failed", "downstream_response_code", resp.Code.String(), "downstream_response_ok", resp.OK, "error", err)
		_ = inbound.Close()
		return
	}
	if !resp.OK {
		logger.Warn("relay downstream rejected", "downstream_response_code", resp.Code.String(), "downstream_message", resp.Message)
		_ = inbound.Close()
		return
	}
	logger.Info("relay stream connected", "downstream_response_code", resp.Code.String())
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
