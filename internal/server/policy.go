package server

import (
	"fmt"

	"github.com/awithy/qoru/internal/config"
)

func isTCPTargetAllowed(cfg *config.Config, target string) bool {
	if len(cfg.AllowedTargets) == 0 {
		return true
	}

	for _, allowed := range cfg.AllowedTargets {
		if allowed.Protocol == "tcp" && target == allowed.Address {
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
