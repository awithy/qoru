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

type Frame struct {
	Version uint8
	Type    MessageType
	Payload []byte
}

type ConnectRequest struct {
	Protocol string
	Target   string
}

type ConnectResponse struct {
	OK      bool
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
	if req.Target == "" {
		return fmt.Errorf("target is required")
	}
	if len(req.Target) > MaxTargetLength {
		return fmt.Errorf("target too long: %d > %d", len(req.Target), MaxTargetLength)
	}

	payload := make([]byte, 1+len(req.Protocol)+2+len(req.Target))
	payload[0] = uint8(len(req.Protocol))
	copy(payload[1:], req.Protocol)
	targetOffset := 1 + len(req.Protocol)
	binary.BigEndian.PutUint16(payload[targetOffset:targetOffset+2], uint16(len(req.Target)))
	copy(payload[targetOffset+2:], req.Target)

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
		return ConnectRequest{}, fmt.Errorf("malformed connect payload: missing target length")
	}

	protocol := string(frame.Payload[1 : 1+protocolLen])
	targetLenOffset := 1 + protocolLen
	targetLen := int(binary.BigEndian.Uint16(frame.Payload[targetLenOffset : targetLenOffset+2]))
	if targetLen == 0 {
		return ConnectRequest{}, fmt.Errorf("target is required")
	}
	if targetLen > MaxTargetLength {
		return ConnectRequest{}, fmt.Errorf("target too long: %d > %d", targetLen, MaxTargetLength)
	}
	if len(frame.Payload[targetLenOffset+2:]) != targetLen {
		return ConnectRequest{}, fmt.Errorf("malformed connect payload: target length mismatch")
	}

	return ConnectRequest{Protocol: protocol, Target: string(frame.Payload[targetLenOffset+2:])}, nil
}

func WriteConnectResponse(w io.Writer, resp ConnectResponse) error {
	status := ConnectStatusOK
	if !resp.OK {
		status = ConnectStatusError
	}
	if len(resp.Message) > MaxPayloadSize-3 {
		return fmt.Errorf("message too long: %d > %d", len(resp.Message), MaxPayloadSize-3)
	}

	payload := make([]byte, 3+len(resp.Message))
	payload[0] = status
	binary.BigEndian.PutUint16(payload[1:3], uint16(len(resp.Message)))
	copy(payload[3:], resp.Message)
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
	if len(frame.Payload) < 3 {
		return ConnectResponse{}, fmt.Errorf("malformed connect response payload")
	}

	status := frame.Payload[0]
	messageLen := int(binary.BigEndian.Uint16(frame.Payload[1:3]))
	if len(frame.Payload[3:]) != messageLen {
		return ConnectResponse{}, fmt.Errorf("malformed connect response payload: message length mismatch")
	}

	switch status {
	case ConnectStatusOK:
		return ConnectResponse{OK: true, Message: string(frame.Payload[3:])}, nil
	case ConnectStatusError:
		return ConnectResponse{OK: false, Message: string(frame.Payload[3:])}, nil
	default:
		return ConnectResponse{}, fmt.Errorf("unknown connect response status %d", status)
	}
}
