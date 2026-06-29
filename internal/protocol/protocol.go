package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	Version           uint8 = 1
	MaxPayloadSize          = 64*1024 - 1
	MaxProtocolLength       = 32
	MaxTargetLength         = 4096
)

type MessageType uint8

const (
	TypeConnectRequest  MessageType = 1
	TypeConnectResponse MessageType = 2
)

const (
	ConnectStatusOK    uint8 = 0
	ConnectStatusError uint8 = 1
)

type ConnectCode uint8

const (
	ConnectCodeOK ConnectCode = iota
	ConnectCodeServiceNotFound
	ConnectCodeAccessDenied
	ConnectCodeTargetDialFailed
	ConnectCodeUnsupportedProtocol
	ConnectCodeUnreachableEgress
	ConnectCodeRouteInvalid
	ConnectCodeNextHopUnreachable
	ConnectCodeInternalError
)

func (c ConnectCode) Valid() bool {
	return c <= ConnectCodeInternalError
}

func (c ConnectCode) String() string {
	switch c {
	case ConnectCodeOK:
		return "OK"
	case ConnectCodeServiceNotFound:
		return "SERVICE_NOT_FOUND"
	case ConnectCodeAccessDenied:
		return "ACCESS_DENIED"
	case ConnectCodeTargetDialFailed:
		return "TARGET_DIAL_FAILED"
	case ConnectCodeUnsupportedProtocol:
		return "UNSUPPORTED_PROTOCOL"
	case ConnectCodeUnreachableEgress:
		return "UNREACHABLE_EGRESS"
	case ConnectCodeRouteInvalid:
		return "ROUTE_INVALID"
	case ConnectCodeNextHopUnreachable:
		return "NEXT_HOP_UNREACHABLE"
	case ConnectCodeInternalError:
		return "INTERNAL_ERROR"
	default:
		return fmt.Sprintf("UNKNOWN_%d", c)
	}
}

type Frame struct {
	Version uint8
	Type    MessageType
	Payload []byte
}

type ConnectRequest struct {
	Protocol string
	Service  string
	Egress   string
}

type ConnectResponse struct {
	OK      bool
	Code    ConnectCode
	Message string
}

func WriteFrame(w io.Writer, typ MessageType, payload []byte) error {
	if len(payload) > MaxPayloadSize {
		return fmt.Errorf("payload too large: %d > %d", len(payload), MaxPayloadSize)
	}

	var header [4]byte
	header[0] = Version
	header[1] = byte(typ)
	binary.BigEndian.PutUint16(header[2:4], uint16(len(payload)))

	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func ReadFrame(r io.Reader) (Frame, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Frame{}, err
	}

	version := header[0]
	if version != Version {
		return Frame{}, fmt.Errorf("unsupported protocol version %d", version)
	}

	length := int(binary.BigEndian.Uint16(header[2:4]))
	if length > MaxPayloadSize {
		return Frame{}, fmt.Errorf("payload too large: %d > %d", length, MaxPayloadSize)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Frame{}, err
	}

	return Frame{Version: version, Type: MessageType(header[1]), Payload: payload}, nil
}

func WriteConnectRequest(w io.Writer, req ConnectRequest) error {
	if req.Protocol == "" {
		return fmt.Errorf("protocol is required")
	}
	if len(req.Protocol) > MaxProtocolLength {
		return fmt.Errorf("protocol too long: %d > %d", len(req.Protocol), MaxProtocolLength)
	}
	if req.Service == "" {
		return fmt.Errorf("service is required")
	}
	if len(req.Service) > MaxTargetLength {
		return fmt.Errorf("service too long: %d > %d", len(req.Service), MaxTargetLength)
	}
	if len(req.Egress) > MaxTargetLength {
		return fmt.Errorf("egress too long: %d > %d", len(req.Egress), MaxTargetLength)
	}

	payload := make([]byte, 1+len(req.Protocol)+2+len(req.Service)+2+len(req.Egress))
	payload[0] = uint8(len(req.Protocol))
	copy(payload[1:], req.Protocol)
	offset := 1 + len(req.Protocol)
	binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(req.Service)))
	copy(payload[offset+2:], req.Service)
	offset += 2 + len(req.Service)
	binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(req.Egress)))
	copy(payload[offset+2:], req.Egress)

	return WriteFrame(w, TypeConnectRequest, payload)
}

func ReadConnectRequest(r io.Reader) (ConnectRequest, error) {
	frame, err := ReadFrame(r)
	if err != nil {
		return ConnectRequest{}, err
	}
	if frame.Type != TypeConnectRequest {
		return ConnectRequest{}, fmt.Errorf("unexpected message type %d", frame.Type)
	}
	if len(frame.Payload) < 3 {
		return ConnectRequest{}, fmt.Errorf("malformed connect payload")
	}

	protocolLen := int(frame.Payload[0])
	if protocolLen == 0 {
		return ConnectRequest{}, fmt.Errorf("protocol is required")
	}
	if protocolLen > MaxProtocolLength {
		return ConnectRequest{}, fmt.Errorf("protocol too long: %d > %d", protocolLen, MaxProtocolLength)
	}
	if len(frame.Payload) < 1+protocolLen+2 {
		return ConnectRequest{}, fmt.Errorf("malformed connect payload: missing service length")
	}

	protocol := string(frame.Payload[1 : 1+protocolLen])
	offset := 1 + protocolLen
	serviceLen := int(binary.BigEndian.Uint16(frame.Payload[offset : offset+2]))
	if serviceLen == 0 {
		return ConnectRequest{}, fmt.Errorf("service is required")
	}
	if serviceLen > MaxTargetLength {
		return ConnectRequest{}, fmt.Errorf("service too long: %d > %d", serviceLen, MaxTargetLength)
	}
	if len(frame.Payload) < offset+2+serviceLen+2 {
		return ConnectRequest{}, fmt.Errorf("malformed connect payload: missing egress length")
	}
	service := string(frame.Payload[offset+2 : offset+2+serviceLen])
	offset += 2 + serviceLen
	egressLen := int(binary.BigEndian.Uint16(frame.Payload[offset : offset+2]))
	if egressLen > MaxTargetLength {
		return ConnectRequest{}, fmt.Errorf("egress too long: %d > %d", egressLen, MaxTargetLength)
	}
	if len(frame.Payload[offset+2:]) != egressLen {
		return ConnectRequest{}, fmt.Errorf("malformed connect payload: egress length mismatch")
	}

	return ConnectRequest{Protocol: protocol, Service: service, Egress: string(frame.Payload[offset+2:])}, nil
}

func WriteConnectResponse(w io.Writer, resp ConnectResponse) error {
	status := ConnectStatusOK
	code := resp.Code
	if !resp.OK {
		status = ConnectStatusError
		if code == ConnectCodeOK {
			code = ConnectCodeInternalError
		}
	} else {
		code = ConnectCodeOK
	}
	if len(resp.Message) > MaxPayloadSize-4 {
		return fmt.Errorf("message too long: %d > %d", len(resp.Message), MaxPayloadSize-4)
	}

	payload := make([]byte, 4+len(resp.Message))
	payload[0] = status
	payload[1] = byte(code)
	binary.BigEndian.PutUint16(payload[2:4], uint16(len(resp.Message)))
	copy(payload[4:], resp.Message)
	return WriteFrame(w, TypeConnectResponse, payload)
}

func ReadConnectResponse(r io.Reader) (ConnectResponse, error) {
	frame, err := ReadFrame(r)
	if err != nil {
		return ConnectResponse{}, err
	}
	if frame.Type != TypeConnectResponse {
		return ConnectResponse{}, fmt.Errorf("unexpected message type %d", frame.Type)
	}
	if len(frame.Payload) < 4 {
		return ConnectResponse{}, fmt.Errorf("malformed connect response payload")
	}

	status := frame.Payload[0]
	code := ConnectCode(frame.Payload[1])
	if !code.Valid() {
		return ConnectResponse{}, fmt.Errorf("unknown connect response code %d", code)
	}
	messageLen := int(binary.BigEndian.Uint16(frame.Payload[2:4]))
	if len(frame.Payload[4:]) != messageLen {
		return ConnectResponse{}, fmt.Errorf("malformed connect response payload: message length mismatch")
	}
	message := string(frame.Payload[4:])

	switch status {
	case ConnectStatusOK:
		if code != ConnectCodeOK {
			return ConnectResponse{}, fmt.Errorf("connect response OK with non-OK code %s", code)
		}
		return ConnectResponse{OK: true, Code: ConnectCodeOK, Message: message}, nil
	case ConnectStatusError:
		if code == ConnectCodeOK {
			return ConnectResponse{}, fmt.Errorf("connect response error with OK code")
		}
		return ConnectResponse{OK: false, Code: code, Message: message}, nil
	default:
		return ConnectResponse{}, fmt.Errorf("unknown connect response status %d", status)
	}
}
