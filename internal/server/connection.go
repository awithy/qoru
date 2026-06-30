package server

import (
	"errors"
	"fmt"
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
		if rt.logger != nil {
			rt.logger.Error("extract peer identity failed", "error", err)
		}
		_ = conn.CloseWithError(1, "peer identity required")
		return
	}
	registeredPeer := false
	if rt.peers != nil {
		registeredPeer = rt.peers.RegisterInbound(peerID, conn)
	}
	if rt.logger != nil {
		if !registeredPeer {
			rt.logger.Info("peer connected", "peer_id", peerID)
		}
		defer rt.logger.Info("peer disconnected", "peer_id", peerID)
	}

	var streamWG sync.WaitGroup
	for {
		stream, err := conn.AcceptStream(rt.ctx)
		if err != nil {
			if rt.ctx.Err() == nil && rt.logger != nil {
				rt.logger.Error("accept stream failed", "error", err)
			}
			_ = conn.CloseWithError(0, "done")
			if waitErr := waitGroupTimeout(&streamWG, defaultShutdownWaitTimeout); waitErr != nil && rt.logger != nil {
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
		if rt.logger != nil {
			rt.logger.Error("read connect request failed", "error", err)
		}
		_ = stream.Close()
		return
	}

	if rt.logger != nil {
		rt.logger.Info("connect requested", "peer_id", peerID, "protocol", req.Protocol, "service", req.Service, "egress", req.Egress)
	}
	if rt.opts.connectRequest != nil {
		rt.opts.connectRequest(req)
	}

	if req.Protocol != "tcp" {
		err := fmt.Errorf("unsupported connect protocol %q", req.Protocol)
		if rt.logger != nil {
			rt.logger.Warn("connect protocol unsupported", "peer_id", peerID, "protocol", req.Protocol, "service", req.Service, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeUnsupportedProtocol, Message: err.Error()})
		_ = stream.Close()
		return
	}

	if err := validateConnectRoute(rt.cfg.NodeID, req.Route, req.Egress); err != nil {
		if rt.logger != nil {
			rt.logger.Warn("route invalid", "peer_id", peerID, "route", req.Route, "egress", req.Egress, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeRouteInvalid, Message: err.Error()})
		_ = stream.Close()
		return
	}

	if len(req.Route) > 1 {
		if err := authorizeRelayIngress(rt.cfg, peerID); err != nil {
			if rt.logger != nil {
				rt.logger.Warn("relay ingress denied", "peer_id", peerID, "route", req.Route, "egress", req.Egress, "error", err)
			}
			_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeAccessDenied, Message: err.Error()})
			_ = stream.Close()
			return
		}
		rt.handleRelayStream(peerID, req, stream)
		return
	}

	if len(req.Route) == 1 {
		if err := requireConfiguredPeer(rt.cfg, peerID); err != nil {
			if rt.logger != nil {
				rt.logger.Warn("routed egress peer denied", "peer_id", peerID, "route", req.Route, "egress", req.Egress, "error", err)
			}
			_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeAccessDenied, Message: err.Error()})
			_ = stream.Close()
			return
		}
	}

	if req.Egress != "" && req.Egress != rt.cfg.NodeID {
		err := fmt.Errorf("egress %q is not reachable from one-hop server %q", req.Egress, rt.cfg.NodeID)
		if rt.logger != nil {
			rt.logger.Warn("egress unsupported", "peer_id", peerID, "egress", req.Egress, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeUnreachableEgress, Message: err.Error()})
		_ = stream.Close()
		return
	}

	svc, err := resolveService(rt.cfg, peerID, req.Protocol, req.Service)
	if err != nil {
		if rt.logger != nil {
			rt.logger.Warn("service denied", "peer_id", peerID, "protocol", req.Protocol, "service", req.Service, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: serviceErrorCode(err), Message: err.Error()})
		_ = stream.Close()
		return
	}

	targetConn, err := dialTCP(rt.ctx, svc.Target)
	if err != nil {
		if rt.logger != nil {
			rt.logger.Error("tcp target dial failed", "peer_id", peerID, "service", svc.Name, "target", svc.Target, "error", err)
		}
		_ = protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeTargetDialFailed, Message: err.Error()})
		_ = stream.Close()
		return
	}
	defer targetConn.Close()

	if err := protocol.WriteConnectResponse(stream, protocol.ConnectResponse{OK: true}); err != nil {
		if rt.logger != nil {
			rt.logger.Error("write connect tcp response failed", "peer_id", peerID, "service", svc.Name, "target", svc.Target, "error", err)
		}
		_ = stream.Close()
		return
	}

	if rt.logger != nil {
		rt.logger.Info("tcp target connected", "peer_id", peerID, "service", svc.Name, "target", svc.Target)
	}

	proxyTCP(stream, targetConn)

	if rt.logger != nil {
		rt.logger.Info("tcp proxy closed", "peer_id", peerID, "service", svc.Name, "target", svc.Target)
	}
}

func (rt *serverRuntime) handleRelayStream(peerID string, req protocol.ConnectRequest, inbound *quic.Stream) {
	nextHop := req.Route[1]
	if rt.peers == nil {
		err := fmt.Errorf("peer sessions are not initialized")
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeInternalError, Message: err.Error()})
		_ = inbound.Close()
		return
	}

	outbound, err := rt.peers.OpenStream(rt.ctx, nextHop)
	if err != nil {
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeNextHopUnreachable, Message: err.Error()})
		_ = inbound.Close()
		return
	}
	defer outbound.Close()

	forwarded := req
	forwarded.Route = append([]string(nil), req.Route[1:]...)
	if err := protocol.WriteConnectRequest(outbound, forwarded); err != nil {
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeNextHopUnreachable, Message: err.Error()})
		_ = inbound.Close()
		return
	}
	resp, err := protocol.ReadConnectResponse(outbound)
	if err != nil {
		_ = protocol.WriteConnectResponse(inbound, protocol.ConnectResponse{OK: false, Code: protocol.ConnectCodeNextHopUnreachable, Message: err.Error()})
		_ = inbound.Close()
		return
	}
	if err := protocol.WriteConnectResponse(inbound, resp); err != nil || !resp.OK {
		_ = inbound.Close()
		return
	}
	if rt.logger != nil {
		rt.logger.Info("relay stream connected", "peer_id", peerID, "next_hop", nextHop, "egress", req.Egress, "service", req.Service)
	}
	proxyStreams(inbound, outbound)
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
