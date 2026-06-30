package requestid

import (
	"fmt"

	"github.com/google/uuid"
)

const Version = 7

func New() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func ParseBytes(s string) ([16]byte, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return [16]byte{}, err
	}
	if id.Version() != Version {
		return [16]byte{}, fmt.Errorf("request_id UUID version must be %d", Version)
	}
	return [16]byte(id), nil
}

func FromBytes(b []byte) (string, error) {
	id, err := uuid.FromBytes(b)
	if err != nil {
		return "", err
	}
	if id.Version() != Version {
		return "", fmt.Errorf("request_id UUID version must be %d", Version)
	}
	return id.String(), nil
}
