package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	Version         uint8 = 1
	MaxPayloadSize      = 64*1024 - 1
	MaxTargetLength     = 4096
)

type MessageType uint8

const (
	TypeConnectTCP MessageType = 1
)

type Frame struct {
	Version uint8
	Type    MessageType
	Payload []byte
}

type ConnectTCPRequest struct {
	Target string
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

func WriteConnectTCPRequest(w io.Writer, req ConnectTCPRequest) error {
	if req.Target == "" {
		return fmt.Errorf("target is required")
	}
	if len(req.Target) > MaxTargetLength {
		return fmt.Errorf("target too long: %d > %d", len(req.Target), MaxTargetLength)
	}

	payload := make([]byte, 2+len(req.Target))
	binary.BigEndian.PutUint16(payload[:2], uint16(len(req.Target)))
	copy(payload[2:], req.Target)

	return WriteFrame(w, TypeConnectTCP, payload)
}

func ReadConnectTCPRequest(r io.Reader) (ConnectTCPRequest, error) {
	frame, err := ReadFrame(r)
	if err != nil {
		return ConnectTCPRequest{}, err
	}
	if frame.Type != TypeConnectTCP {
		return ConnectTCPRequest{}, fmt.Errorf("unexpected message type %d", frame.Type)
	}
	if len(frame.Payload) < 2 {
		return ConnectTCPRequest{}, fmt.Errorf("malformed connect tcp payload: missing target length")
	}

	targetLen := int(binary.BigEndian.Uint16(frame.Payload[:2]))
	if targetLen == 0 {
		return ConnectTCPRequest{}, fmt.Errorf("target is required")
	}
	if targetLen > MaxTargetLength {
		return ConnectTCPRequest{}, fmt.Errorf("target too long: %d > %d", targetLen, MaxTargetLength)
	}
	if len(frame.Payload[2:]) != targetLen {
		return ConnectTCPRequest{}, fmt.Errorf("malformed connect tcp payload: target length mismatch")
	}

	return ConnectTCPRequest{Target: string(frame.Payload[2:])}, nil
}
