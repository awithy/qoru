package config

import "os"

const (
	ModeClient = "client"
	ModeServer = "server"
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

	Identity IdentityConfig `yaml:"identity"`

	Server *ServerConfig `yaml:"server,omitempty"`
	Listen string        `yaml:"listen,omitempty"`

	TCPForwards []TCPForwardConfig `yaml:"tcp_forwards,omitempty"`
}

type IdentityConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
	CA   string `yaml:"ca"`
}

type ServerConfig struct {
	ID      string `yaml:"id"`
	Address string `yaml:"address"`
}

type TCPForwardConfig struct {
	Listen string `yaml:"listen"`
	Target string `yaml:"target"`
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
