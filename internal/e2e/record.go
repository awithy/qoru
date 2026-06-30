package e2e

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/awithy/qoru/internal/protocol"
)

const (
	CloseCodeOK    uint8 = 0
	CloseCodeError uint8 = 1

	recordNonceSuffixSize = 8
	recordDomain          = "qoru-e2e-data-v1"
)

var ErrEncryptedWriterClosed = errors.New("e2e encrypted writer closed")

type CloseError struct {
	Code    uint8
	Message string
}

func (e *CloseError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("e2e stream closed with code %d", e.Code)
	}
	return fmt.Sprintf("e2e stream closed with code %d: %s", e.Code, e.Message)
}

// EncryptedReader converts E2EData frames from r into plaintext bytes.
// It returns io.EOF after an E2EClose frame with code 0.
type EncryptedReader struct {
	r              io.Reader
	aead           cipher.AEAD
	transcriptHash []byte
	seq            uint64
	buf            bytes.Buffer
	closed         bool
}

// EncryptedWriter converts plaintext writes into E2EData frames on w.
type EncryptedWriter struct {
	w              io.Writer
	aead           cipher.AEAD
	transcriptHash []byte
	maxPlaintext   int

	mu     sync.Mutex
	seq    uint64
	closed bool
}

func NewEncryptedReader(r io.Reader, key, transcriptHash []byte) (*EncryptedReader, error) {
	if r == nil {
		return nil, fmt.Errorf("reader is required")
	}
	aead, err := newRecordAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(transcriptHash) == 0 {
		return nil, fmt.Errorf("transcript hash is required")
	}
	return &EncryptedReader{r: r, aead: aead, transcriptHash: append([]byte(nil), transcriptHash...)}, nil
}

func NewEncryptedWriter(w io.Writer, key, transcriptHash []byte) (*EncryptedWriter, error) {
	if w == nil {
		return nil, fmt.Errorf("writer is required")
	}
	aead, err := newRecordAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(transcriptHash) == 0 {
		return nil, fmt.Errorf("transcript hash is required")
	}
	maxPlaintext := protocol.MaxE2ECiphertextLength - aead.Overhead()
	if maxPlaintext <= 0 {
		return nil, fmt.Errorf("encrypted record plaintext limit is invalid")
	}
	return &EncryptedWriter{w: w, aead: aead, transcriptHash: append([]byte(nil), transcriptHash...), maxPlaintext: maxPlaintext}, nil
}

func (r *EncryptedReader) Read(p []byte) (int, error) {
	if r.buf.Len() > 0 {
		return r.buf.Read(p)
	}
	if r.closed {
		return 0, io.EOF
	}
	for r.buf.Len() == 0 {
		frame, err := protocol.ReadFrame(r.r)
		if err != nil {
			return 0, err
		}
		switch frame.Type {
		case protocol.TypeE2EData:
			plaintext, err := r.openDataFrame(frame.Payload)
			if err != nil {
				return 0, err
			}
			if len(plaintext) == 0 {
				continue
			}
			_, _ = r.buf.Write(plaintext)
		case protocol.TypeE2EClose:
			closeFrame, err := protocol.DecodeE2EClosePayload(frame.Payload)
			if err != nil {
				return 0, err
			}
			r.closed = true
			if closeFrame.Code == CloseCodeOK {
				return 0, io.EOF
			}
			return 0, &CloseError{Code: closeFrame.Code, Message: closeFrame.Message}
		default:
			return 0, fmt.Errorf("unexpected e2e frame type %d", frame.Type)
		}
	}
	return r.buf.Read(p)
}

func (r *EncryptedReader) openDataFrame(payload []byte) ([]byte, error) {
	data, err := protocol.DecodeE2EDataPayload(payload)
	if err != nil {
		return nil, err
	}
	expected := nonceSuffix(r.seq)
	if !bytes.Equal(data.NonceSuffix, expected) {
		return nil, fmt.Errorf("unexpected e2e record sequence: got %d want %d", suffixSequence(data.NonceSuffix), r.seq)
	}
	plaintext, err := r.aead.Open(nil, nonceFromSuffix(data.NonceSuffix, r.aead.NonceSize()), data.Ciphertext, recordAAD(r.transcriptHash, data.NonceSuffix))
	if err != nil {
		return nil, fmt.Errorf("decrypt e2e record: %w", err)
	}
	if r.seq == math.MaxUint64 {
		return nil, fmt.Errorf("e2e record sequence exhausted")
	}
	r.seq++
	return plaintext, nil
}

func (w *EncryptedWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, ErrEncryptedWriterClosed
	}
	written := 0
	for written < len(p) {
		end := written + w.maxPlaintext
		if end > len(p) {
			end = len(p)
		}
		if err := w.writeRecordLocked(p[written:end]); err != nil {
			return written, err
		}
		written = end
	}
	return written, nil
}

func (w *EncryptedWriter) CloseWrite() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return protocol.WriteE2EClose(w.w, protocol.E2EClose{Code: CloseCodeOK})
}

func (w *EncryptedWriter) Close() error {
	return w.CloseWrite()
}

func (w *EncryptedWriter) writeRecordLocked(plaintext []byte) error {
	if w.seq == math.MaxUint64 {
		return fmt.Errorf("e2e record sequence exhausted")
	}
	suffix := nonceSuffix(w.seq)
	ciphertext := w.aead.Seal(nil, nonceFromSuffix(suffix, w.aead.NonceSize()), plaintext, recordAAD(w.transcriptHash, suffix))
	if err := protocol.WriteE2EData(w.w, protocol.E2EData{NonceSuffix: suffix, Ciphertext: ciphertext}); err != nil {
		return err
	}
	w.seq++
	return nil
}

func newRecordAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create e2e record cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create e2e record AEAD: %w", err)
	}
	return aead, nil
}

func nonceSuffix(seq uint64) []byte {
	var suffix [recordNonceSuffixSize]byte
	binary.BigEndian.PutUint64(suffix[:], seq)
	return suffix[:]
}

func suffixSequence(suffix []byte) uint64 {
	if len(suffix) != recordNonceSuffixSize {
		return 0
	}
	return binary.BigEndian.Uint64(suffix)
}

func nonceFromSuffix(suffix []byte, nonceSize int) []byte {
	nonce := make([]byte, nonceSize)
	copy(nonce[nonceSize-len(suffix):], suffix)
	return nonce
}

func recordAAD(transcriptHash, suffix []byte) []byte {
	var aad bytes.Buffer
	writeString(&aad, recordDomain)
	writeBytes(&aad, transcriptHash)
	writeBytes(&aad, suffix)
	return aad.Bytes()
}
