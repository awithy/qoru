package server

import (
	"fmt"

	"github.com/awithy/qoru/internal/config"
)

func isTCPTargetAllowed(cfg *config.Config, peerID, target string) bool {
	if len(cfg.AllowedTargets) == 0 {
		return true
	}

	for _, allowed := range cfg.AllowedTargets {
		if allowed.Protocol != "tcp" || target != allowed.Address {
			continue
		}
		if len(allowed.Peers) == 0 {
			return true
		}
		for _, peer := range allowed.Peers {
			if peerID == peer {
				return true
			}
		}
	}
	return false
}

func authorizeTCPTarget(cfg *config.Config, peerID, target string) error {
	if isTCPTargetAllowed(cfg, peerID, target) {
		return nil
	}
	return fmt.Errorf("peer %q is not allowed to access tcp target %q", peerID, target)
}
