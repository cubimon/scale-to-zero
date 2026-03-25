package registry

import (
	"log"
	"os"
	"sync"
	"time"

	"go.yaml.in/yaml/v2"
)

type ServiceState int

const (
	Unknown ServiceState = iota
	StateStopped
	StateStarting
	StateStarted
	StateStopping
)

type Service struct {
	Image         string `yaml:"image"`
	Host          string `yaml:"host"`
	ProxyIp       string `yaml:"ip"` // TODO: autoassign
	ContainerPort int    `yaml:"container_port"`
	state         ServiceState
	containerIp   string
	activeCount   int
	lastActive    time.Time
	mu            sync.Mutex
}

type Config struct {
	Services []Service `yaml:"services"`
}

// loadConfig reads the yaml file and returns the populated Config struct
func loadConfig(path string) *Config {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("CRITICAL: Could not read config file at %s: %v", path, err)
	}
	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		log.Fatalf("CRITICAL: Failed to parse YAML: %v", err)
	}
	return &cfg
}
