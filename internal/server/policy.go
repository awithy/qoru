package server

import (
	"fmt"

	"github.com/awithy/qoru/internal/config"
)

func isTCPTargetAllowed(cfg *config.Config, target string) bool {
	if len(cfg.AllowedTCPTargets) == 0 {
		return true
	}

	for _, allowed := range cfg.AllowedTCPTargets {
		if target == allowed {
			return true
		}
	}
	return false
}

func authorizeTCPTarget(cfg *config.Config, target string) error {
	if isTCPTargetAllowed(cfg, target) {
		return nil
	}
	return fmt.Errorf("tcp target %q is not allowed", target)
}
