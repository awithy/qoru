package config

import "os"

const (
	ModeClient = "client"
	ModeServer = "server"

	ForwardE2EOff    = "off"
	ForwardE2EAuto   = "auto"
	ForwardE2EAlways = "always"
)

var DefaultPaths = []string{
	"./qoru.yaml",
	"./qoru.yml",
	"/etc/qoru/config.yaml",
	"/etc/qoru/config.yml",
}

type Config struct {
	NodeID string `yaml:"node_id"`
	Mode   string `yaml:"mode"`

	Identity        IdentityConfig        `yaml:"identity"`
	ServiceIdentity ServiceIdentityConfig `yaml:"service_identity,omitempty"`

	Servers []ServerConfig `yaml:"servers,omitempty"`
	Peers   []PeerConfig   `yaml:"peers,omitempty"`
	Listen  string         `yaml:"listen,omitempty"`

	Forwards            []ForwardConfig      `yaml:"forwards,omitempty"`
	Routes              []ServiceRouteConfig `yaml:"routes,omitempty"`
	Services            []ServiceConfig      `yaml:"services,omitempty"`
	AllowedRelayClients []string             `yaml:"allowed_relay_clients,omitempty"`
}

type IdentityConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
	CA   string `yaml:"ca"`
}

type ServiceIdentityConfig struct {
	CA string `yaml:"ca,omitempty"`
}

type ServerConfig struct {
	ID      string `yaml:"id"`
	Address string `yaml:"address"`
}

type PeerConfig struct {
	ID      string `yaml:"id"`
	Address string `yaml:"address,omitempty"`
	Dial    bool   `yaml:"dial,omitempty"`
}

type ForwardConfig struct {
	Protocol string   `yaml:"protocol"`
	Listen   string   `yaml:"listen"`
	Service  string   `yaml:"service"`
	Egress   string   `yaml:"egress,omitempty"`
	Route    []string `yaml:"route,omitempty"`
	E2E      string   `yaml:"e2e,omitempty"`
}

type ServiceRouteConfig struct {
	Service    string                 `yaml:"service"`
	Protocol   string                 `yaml:"protocol"`
	Selection  string                 `yaml:"selection,omitempty"`
	Candidates []RouteCandidateConfig `yaml:"candidates"`
}

type RouteCandidateConfig struct {
	Egress string   `yaml:"egress"`
	Route  []string `yaml:"route"`
}

type ServiceConfig struct {
	Name     string           `yaml:"name"`
	Protocol string           `yaml:"protocol"`
	Target   string           `yaml:"target"`
	Peers    []string         `yaml:"peers,omitempty"`
	E2E      ServiceE2EConfig `yaml:"e2e,omitempty"`
}

type ServiceE2EConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

func ResolvePath(explicit string) (string, bool) {
	return ResolvePathWithDefaults(explicit, DefaultPaths)
}

func ResolvePathWithDefaults(explicit string, defaults []string) (string, bool) {
	if explicit != "" {
		return explicit, true
	}

	for _, path := range defaults {
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}

	return "", false
}
