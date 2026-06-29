package identity

import (
	"crypto/tls"
	"fmt"
	"strings"
)

const spiffeNodePrefix = "spiffe://qoru/node/"

func PeerNodeID(state tls.ConnectionState) (string, error) {
	if len(state.PeerCertificates) == 0 {
		return "", fmt.Errorf("peer certificate is required")
	}

	cert := state.PeerCertificates[0]
	for _, uri := range cert.URIs {
		value := uri.String()
		if strings.HasPrefix(value, spiffeNodePrefix) {
			nodeID := strings.TrimPrefix(value, spiffeNodePrefix)
			if nodeID != "" {
				return nodeID, nil
			}
		}
	}

	return "", fmt.Errorf("peer certificate does not contain a SPIFFE node identity")
}
