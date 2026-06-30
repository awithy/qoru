package protocol

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/awithy/qoru/internal/requestid"
)

const (
	Version           uint8 = 1
	MaxPayloadSize          = 64*1024 - 1
	MaxProtocolLength       = 32
	MaxTargetLength         = 4096
)

type MessageType uint8

const (
	TypeConnectRequest    MessageType = 1
	TypeConnectResponse   MessageType = 2
	TypeE2EClientHello    MessageType = 3
	TypeE2EServerHello    MessageType = 4
	TypeE2EData           MessageType = 5
	TypeE2EClose          MessageType = 6
	TypeE2EClientFinished MessageType = 7
)

const (
	ConnectStatusOK    uint8 = 0
	ConnectStatusError uint8 = 1
)

const (
	MaxE2ECertChainCount          = 8
	MaxE2ECertLength              = 16 * 1024
	MaxE2EEphemeralKeyLength      = 4096
	MaxE2ESignatureLength         = 8192
	MaxE2EFinishedSignatureLength = MaxE2ESignatureLength
	MaxE2ENonceSuffixLength       = 24
	MaxE2ECiphertextLength        = MaxPayloadSize - 3
	MaxE2ECloseMessageLength      = MaxPayloadSize - 3
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
	RequestID string
	Protocol  string
	Service   string
	Egress    string
	Route     []string
}

type ConnectResponse struct {
	OK      bool
	Code    ConnectCode
	Message string
}

type E2EClientHello struct {
	ClientCertChain    [][]byte
	EphemeralPublicKey []byte
	Signature          []byte
}

type E2EServerHello struct {
	ServiceCertChain   [][]byte
	EphemeralPublicKey []byte
	Signature          []byte
}

type E2EData struct {
	NonceSuffix []byte
	Ciphertext  []byte
}

type E2EClose struct {
	Code    uint8
	Message string
}

type E2EClientFinished struct {
	Signature []byte
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
	requestID, err := requestid.ParseBytes(req.RequestID)
	if err != nil {
		return fmt.Errorf("request_id must be a valid UUIDv7: %w", err)
	}
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
	if len(req.Route) > 255 {
		return fmt.Errorf("route too long: %d > %d", len(req.Route), 255)
	}
	routeLen := 1
	for i, hop := range req.Route {
		if hop == "" {
			return fmt.Errorf("route[%d] is required", i)
		}
		if len(hop) > MaxTargetLength {
			return fmt.Errorf("route[%d] too long: %d > %d", i, len(hop), MaxTargetLength)
		}
		routeLen += 2 + len(hop)
	}

	payload := make([]byte, 16+1+len(req.Protocol)+2+len(req.Service)+2+len(req.Egress)+routeLen)
	copy(payload[0:16], requestID[:])
	offset := 16
	payload[offset] = uint8(len(req.Protocol))
	copy(payload[offset+1:], req.Protocol)
	offset += 1 + len(req.Protocol)
	binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(req.Service)))
	copy(payload[offset+2:], req.Service)
	offset += 2 + len(req.Service)
	binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(req.Egress)))
	copy(payload[offset+2:], req.Egress)
	offset += 2 + len(req.Egress)
	payload[offset] = uint8(len(req.Route))
	offset++
	for _, hop := range req.Route {
		binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(hop)))
		copy(payload[offset+2:], hop)
		offset += 2 + len(hop)
	}

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
	if len(frame.Payload) < 19 {
		return ConnectRequest{}, fmt.Errorf("malformed connect payload")
	}

	requestID, err := requestid.FromBytes(frame.Payload[0:16])
	if err != nil {
		return ConnectRequest{}, fmt.Errorf("malformed request_id: %w", err)
	}
	offset := 16
	protocolLen := int(frame.Payload[offset])
	if protocolLen == 0 {
		return ConnectRequest{}, fmt.Errorf("protocol is required")
	}
	if protocolLen > MaxProtocolLength {
		return ConnectRequest{}, fmt.Errorf("protocol too long: %d > %d", protocolLen, MaxProtocolLength)
	}
	if len(frame.Payload) < offset+1+protocolLen+2 {
		return ConnectRequest{}, fmt.Errorf("malformed connect payload: missing service length")
	}

	protocol := string(frame.Payload[offset+1 : offset+1+protocolLen])
	offset += 1 + protocolLen
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
	if len(frame.Payload) < offset+2+egressLen+1 {
		return ConnectRequest{}, fmt.Errorf("malformed connect payload: missing route length")
	}
	egress := string(frame.Payload[offset+2 : offset+2+egressLen])
	offset += 2 + egressLen
	routeCount := int(frame.Payload[offset])
	offset++
	var route []string
	if routeCount > 0 {
		route = make([]string, 0, routeCount)
	}
	for i := 0; i < routeCount; i++ {
		if len(frame.Payload) < offset+2 {
			return ConnectRequest{}, fmt.Errorf("malformed connect payload: missing route[%d] length", i)
		}
		hopLen := int(binary.BigEndian.Uint16(frame.Payload[offset : offset+2]))
		if hopLen == 0 {
			return ConnectRequest{}, fmt.Errorf("route[%d] is required", i)
		}
		if hopLen > MaxTargetLength {
			return ConnectRequest{}, fmt.Errorf("route[%d] too long: %d > %d", i, hopLen, MaxTargetLength)
		}
		if len(frame.Payload) < offset+2+hopLen {
			return ConnectRequest{}, fmt.Errorf("malformed connect payload: route[%d] length mismatch", i)
		}
		route = append(route, string(frame.Payload[offset+2:offset+2+hopLen]))
		offset += 2 + hopLen
	}
	if len(frame.Payload[offset:]) != 0 {
		return ConnectRequest{}, fmt.Errorf("malformed connect payload: trailing bytes")
	}

	return ConnectRequest{RequestID: requestID, Protocol: protocol, Service: service, Egress: egress, Route: route}, nil
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

func WriteE2EClientHello(w io.Writer, hello E2EClientHello) error {
	payload, err := marshalE2EHello(hello.ClientCertChain, hello.EphemeralPublicKey, hello.Signature, "client")
	if err != nil {
		return err
	}
	return WriteFrame(w, TypeE2EClientHello, payload)
}

func ReadE2EClientHello(r io.Reader) (E2EClientHello, error) {
	chain, key, sig, err := readE2EHelloFrame(r, TypeE2EClientHello, "client")
	if err != nil {
		return E2EClientHello{}, err
	}
	return E2EClientHello{ClientCertChain: chain, EphemeralPublicKey: key, Signature: sig}, nil
}

func WriteE2EServerHello(w io.Writer, hello E2EServerHello) error {
	payload, err := marshalE2EHello(hello.ServiceCertChain, hello.EphemeralPublicKey, hello.Signature, "server")
	if err != nil {
		return err
	}
	return WriteFrame(w, TypeE2EServerHello, payload)
}

func ReadE2EServerHello(r io.Reader) (E2EServerHello, error) {
	chain, key, sig, err := readE2EHelloFrame(r, TypeE2EServerHello, "server")
	if err != nil {
		return E2EServerHello{}, err
	}
	return E2EServerHello{ServiceCertChain: chain, EphemeralPublicKey: key, Signature: sig}, nil
}

func marshalE2EHello(certChain [][]byte, ephemeralPublicKey, signature []byte, label string) ([]byte, error) {
	if len(certChain) == 0 {
		return nil, fmt.Errorf("%s cert chain is required", label)
	}
	if len(certChain) > MaxE2ECertChainCount {
		return nil, fmt.Errorf("%s cert chain too long: %d > %d", label, len(certChain), MaxE2ECertChainCount)
	}
	payloadLen := 1
	for i, cert := range certChain {
		if len(cert) == 0 {
			return nil, fmt.Errorf("%s cert chain[%d] is required", label, i)
		}
		if len(cert) > MaxE2ECertLength {
			return nil, fmt.Errorf("%s cert chain[%d] too long: %d > %d", label, i, len(cert), MaxE2ECertLength)
		}
		payloadLen += 2 + len(cert)
	}
	if len(ephemeralPublicKey) == 0 {
		return nil, fmt.Errorf("%s ephemeral public key is required", label)
	}
	if len(ephemeralPublicKey) > MaxE2EEphemeralKeyLength {
		return nil, fmt.Errorf("%s ephemeral public key too long: %d > %d", label, len(ephemeralPublicKey), MaxE2EEphemeralKeyLength)
	}
	if len(signature) == 0 {
		return nil, fmt.Errorf("%s signature is required", label)
	}
	if len(signature) > MaxE2ESignatureLength {
		return nil, fmt.Errorf("%s signature too long: %d > %d", label, len(signature), MaxE2ESignatureLength)
	}
	payloadLen += 2 + len(ephemeralPublicKey) + 2 + len(signature)
	if payloadLen > MaxPayloadSize {
		return nil, fmt.Errorf("%s hello payload too large: %d > %d", label, payloadLen, MaxPayloadSize)
	}

	payload := make([]byte, payloadLen)
	offset := 0
	payload[offset] = uint8(len(certChain))
	offset++
	for _, cert := range certChain {
		binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(cert)))
		copy(payload[offset+2:], cert)
		offset += 2 + len(cert)
	}
	binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(ephemeralPublicKey)))
	copy(payload[offset+2:], ephemeralPublicKey)
	offset += 2 + len(ephemeralPublicKey)
	binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(signature)))
	copy(payload[offset+2:], signature)
	return payload, nil
}

func readE2EHelloFrame(r io.Reader, typ MessageType, label string) ([][]byte, []byte, []byte, error) {
	frame, err := ReadFrame(r)
	if err != nil {
		return nil, nil, nil, err
	}
	if frame.Type != typ {
		return nil, nil, nil, fmt.Errorf("unexpected message type %d", frame.Type)
	}
	return unmarshalE2EHello(frame.Payload, label)
}

func unmarshalE2EHello(payload []byte, label string) ([][]byte, []byte, []byte, error) {
	if len(payload) < 1 {
		return nil, nil, nil, fmt.Errorf("malformed %s hello payload", label)
	}
	offset := 0
	chainCount := int(payload[offset])
	offset++
	if chainCount == 0 {
		return nil, nil, nil, fmt.Errorf("%s cert chain is required", label)
	}
	if chainCount > MaxE2ECertChainCount {
		return nil, nil, nil, fmt.Errorf("%s cert chain too long: %d > %d", label, chainCount, MaxE2ECertChainCount)
	}
	chain := make([][]byte, 0, chainCount)
	for i := 0; i < chainCount; i++ {
		cert, next, err := readLengthPrefixedBytes(payload, offset, MaxE2ECertLength, fmt.Sprintf("%s cert chain[%d]", label, i))
		if err != nil {
			return nil, nil, nil, err
		}
		chain = append(chain, cert)
		offset = next
	}
	ephemeralPublicKey, next, err := readLengthPrefixedBytes(payload, offset, MaxE2EEphemeralKeyLength, label+" ephemeral public key")
	if err != nil {
		return nil, nil, nil, err
	}
	offset = next
	signature, next, err := readLengthPrefixedBytes(payload, offset, MaxE2ESignatureLength, label+" signature")
	if err != nil {
		return nil, nil, nil, err
	}
	offset = next
	if len(payload[offset:]) != 0 {
		return nil, nil, nil, fmt.Errorf("malformed %s hello payload: trailing bytes", label)
	}
	return chain, ephemeralPublicKey, signature, nil
}

func WriteE2EData(w io.Writer, data E2EData) error {
	if len(data.NonceSuffix) == 0 {
		return fmt.Errorf("nonce suffix is required")
	}
	if len(data.NonceSuffix) > MaxE2ENonceSuffixLength {
		return fmt.Errorf("nonce suffix too long: %d > %d", len(data.NonceSuffix), MaxE2ENonceSuffixLength)
	}
	if len(data.Ciphertext) == 0 {
		return fmt.Errorf("ciphertext is required")
	}
	if len(data.Ciphertext) > MaxE2ECiphertextLength {
		return fmt.Errorf("ciphertext too long: %d > %d", len(data.Ciphertext), MaxE2ECiphertextLength)
	}
	payload := make([]byte, 1+len(data.NonceSuffix)+2+len(data.Ciphertext))
	offset := 0
	payload[offset] = uint8(len(data.NonceSuffix))
	offset++
	copy(payload[offset:], data.NonceSuffix)
	offset += len(data.NonceSuffix)
	binary.BigEndian.PutUint16(payload[offset:offset+2], uint16(len(data.Ciphertext)))
	copy(payload[offset+2:], data.Ciphertext)
	return WriteFrame(w, TypeE2EData, payload)
}

func ReadE2EData(r io.Reader) (E2EData, error) {
	frame, err := ReadFrame(r)
	if err != nil {
		return E2EData{}, err
	}
	if frame.Type != TypeE2EData {
		return E2EData{}, fmt.Errorf("unexpected message type %d", frame.Type)
	}
	if len(frame.Payload) < 4 {
		return E2EData{}, fmt.Errorf("malformed e2e data payload")
	}
	offset := 0
	nonceLen := int(frame.Payload[offset])
	if nonceLen == 0 {
		return E2EData{}, fmt.Errorf("nonce suffix is required")
	}
	if nonceLen > MaxE2ENonceSuffixLength {
		return E2EData{}, fmt.Errorf("nonce suffix too long: %d > %d", nonceLen, MaxE2ENonceSuffixLength)
	}
	offset++
	if len(frame.Payload) < offset+nonceLen+2 {
		return E2EData{}, fmt.Errorf("malformed e2e data payload: missing ciphertext length")
	}
	nonce := append([]byte(nil), frame.Payload[offset:offset+nonceLen]...)
	offset += nonceLen
	ciphertextLen := int(binary.BigEndian.Uint16(frame.Payload[offset : offset+2]))
	if ciphertextLen == 0 {
		return E2EData{}, fmt.Errorf("ciphertext is required")
	}
	if ciphertextLen > MaxE2ECiphertextLength {
		return E2EData{}, fmt.Errorf("ciphertext too long: %d > %d", ciphertextLen, MaxE2ECiphertextLength)
	}
	offset += 2
	if len(frame.Payload[offset:]) != ciphertextLen {
		return E2EData{}, fmt.Errorf("malformed e2e data payload: ciphertext length mismatch")
	}
	ciphertext := append([]byte(nil), frame.Payload[offset:]...)
	return E2EData{NonceSuffix: nonce, Ciphertext: ciphertext}, nil
}

func WriteE2EClose(w io.Writer, close E2EClose) error {
	if len(close.Message) > MaxE2ECloseMessageLength {
		return fmt.Errorf("close message too long: %d > %d", len(close.Message), MaxE2ECloseMessageLength)
	}
	payload := make([]byte, 3+len(close.Message))
	payload[0] = close.Code
	binary.BigEndian.PutUint16(payload[1:3], uint16(len(close.Message)))
	copy(payload[3:], close.Message)
	return WriteFrame(w, TypeE2EClose, payload)
}

func WriteE2EClientFinished(w io.Writer, finished E2EClientFinished) error {
	if len(finished.Signature) == 0 {
		return fmt.Errorf("client finished signature is required")
	}
	if len(finished.Signature) > MaxE2EFinishedSignatureLength {
		return fmt.Errorf("client finished signature too long: %d > %d", len(finished.Signature), MaxE2EFinishedSignatureLength)
	}
	payload := make([]byte, 2+len(finished.Signature))
	binary.BigEndian.PutUint16(payload[0:2], uint16(len(finished.Signature)))
	copy(payload[2:], finished.Signature)
	return WriteFrame(w, TypeE2EClientFinished, payload)
}

func ReadE2EClose(r io.Reader) (E2EClose, error) {
	frame, err := ReadFrame(r)
	if err != nil {
		return E2EClose{}, err
	}
	if frame.Type != TypeE2EClose {
		return E2EClose{}, fmt.Errorf("unexpected message type %d", frame.Type)
	}
	if len(frame.Payload) < 3 {
		return E2EClose{}, fmt.Errorf("malformed e2e close payload")
	}
	messageLen := int(binary.BigEndian.Uint16(frame.Payload[1:3]))
	if messageLen > MaxE2ECloseMessageLength {
		return E2EClose{}, fmt.Errorf("close message too long: %d > %d", messageLen, MaxE2ECloseMessageLength)
	}
	if len(frame.Payload[3:]) != messageLen {
		return E2EClose{}, fmt.Errorf("malformed e2e close payload: message length mismatch")
	}
	return E2EClose{Code: frame.Payload[0], Message: string(frame.Payload[3:])}, nil
}

func ReadE2EClientFinished(r io.Reader) (E2EClientFinished, error) {
	frame, err := ReadFrame(r)
	if err != nil {
		return E2EClientFinished{}, err
	}
	if frame.Type != TypeE2EClientFinished {
		return E2EClientFinished{}, fmt.Errorf("unexpected message type %d", frame.Type)
	}
	if len(frame.Payload) < 2 {
		return E2EClientFinished{}, fmt.Errorf("malformed e2e client finished payload")
	}
	signatureLen := int(binary.BigEndian.Uint16(frame.Payload[0:2]))
	if signatureLen == 0 {
		return E2EClientFinished{}, fmt.Errorf("client finished signature is required")
	}
	if signatureLen > MaxE2EFinishedSignatureLength {
		return E2EClientFinished{}, fmt.Errorf("client finished signature too long: %d > %d", signatureLen, MaxE2EFinishedSignatureLength)
	}
	if len(frame.Payload[2:]) != signatureLen {
		return E2EClientFinished{}, fmt.Errorf("malformed e2e client finished payload: signature length mismatch")
	}
	return E2EClientFinished{Signature: append([]byte(nil), frame.Payload[2:]...)}, nil
}

func readLengthPrefixedBytes(payload []byte, offset, maxLen int, field string) ([]byte, int, error) {
	if len(payload) < offset+2 {
		return nil, offset, fmt.Errorf("malformed e2e hello payload: missing %s length", field)
	}
	length := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
	if length == 0 {
		return nil, offset, fmt.Errorf("%s is required", field)
	}
	if length > maxLen {
		return nil, offset, fmt.Errorf("%s too long: %d > %d", field, length, maxLen)
	}
	if len(payload) < offset+2+length {
		return nil, offset, fmt.Errorf("malformed e2e hello payload: %s length mismatch", field)
	}
	value := append([]byte(nil), payload[offset+2:offset+2+length]...)
	return value, offset + 2 + length, nil
}
