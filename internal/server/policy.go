package server

import (
	"fmt"

	"github.com/awithy/qoru/internal/config"
)

func resolveService(cfg *config.Config, peerID, protocol, service string) (config.ServiceConfig, error) {
	for _, svc := range cfg.Services {
		if svc.Protocol != protocol || svc.Name != service {
			continue
		}
		if len(svc.Peers) == 0 {
			return svc, nil
		}
		for _, peer := range svc.Peers {
			if peerID == peer {
				return svc, nil
			}
		}
		return config.ServiceConfig{}, fmt.Errorf("peer %q is not allowed to access %s service %q", peerID, protocol, service)
	}
	return config.ServiceConfig{}, fmt.Errorf("%s service %q not found", protocol, service)
}
