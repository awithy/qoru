package server

import (
	"errors"
	"fmt"

	"github.com/awithy/qoru/internal/config"
)

var (
	ErrServiceNotFound = errors.New("service not found")
	ErrAccessDenied    = errors.New("access denied")
)

func authorizeRelayIngress(cfg *config.Config, peerID string) error {
	if len(cfg.AllowedRelayClients) == 0 {
		return nil
	}
	for _, allowed := range cfg.AllowedRelayClients {
		if allowed == peerID {
			return nil
		}
	}
	return fmt.Errorf("%w: peer %q is not allowed to use this node as a relay", ErrAccessDenied, peerID)
}

func requireConfiguredPeer(cfg *config.Config, peerID string) error {
	for _, peer := range cfg.Peers {
		if peer.ID == peerID {
			return nil
		}
	}
	return fmt.Errorf("%w: peer %q is not configured as a relay peer", ErrAccessDenied, peerID)
}

func resolveService(cfg *config.Config, peerID, protocol, service string) (config.ServiceConfig, error) {
	svc, err := findService(cfg, protocol, service)
	if err != nil {
		return config.ServiceConfig{}, err
	}
	if err := authorizeServicePeer(svc, peerID, protocol, service); err != nil {
		return config.ServiceConfig{}, err
	}
	return svc, nil
}

func findService(cfg *config.Config, protocol, service string) (config.ServiceConfig, error) {
	for _, svc := range cfg.Services {
		if svc.Protocol == protocol && svc.Name == service {
			return svc, nil
		}
	}
	return config.ServiceConfig{}, fmt.Errorf("%w: %s service %q", ErrServiceNotFound, protocol, service)
}

func authorizeServicePeer(svc config.ServiceConfig, peerID, protocol, service string) error {
	if len(svc.Peers) == 0 {
		return nil
	}
	for _, peer := range svc.Peers {
		if peerID == peer {
			return nil
		}
	}
	return fmt.Errorf("%w: peer %q is not allowed to access %s service %q", ErrAccessDenied, peerID, protocol, service)
}
