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
	Image       string `yaml:"image"`
	Host        string `yaml:"host"`
	RemoteIP    string `yaml:"remote_ip"`
	RemotePort  int    `yaml:"remote_port"`
	state       ServiceState
	proxyIp     string
	containerIP string
	activeCount int
	lastActive  time.Time
	mu          sync.Mutex
}

type IPAM struct {
	Start string `yaml:"start"`
	End   string `yaml:"end"`
}

type Janitor struct {
	PollInterval    int    `yaml:"poll_interval"`
	Timeout         int    `yaml:"timeout"`
	PressureTrigger string `yaml:"pressure_trigger"`
	MemoryThreshold int    `yaml:"memory_threshold"`
}

type DNS struct {
	Upstream string `yaml:"upstream"`
}

type Config struct {
	IP       string    `yaml:"ip"`
	Iface    string    `yaml:"iface"`
	IPAM     IPAM      `yaml:"ipam"`
	Janitor  Janitor   `yaml:"janitor"`
	DNS      DNS       `yaml:"dns"`
	Services []Service `yaml:"services"`
}

// loadConfig reads the yaml file and returns the populated Config struct
func loadConfig(path string) Config {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("CRITICAL: Could not read config file at %s: %v", path, err)
	}
	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		log.Fatalf("CRITICAL: Failed to parse YAML: %v", err)
	}
	return cfg
}
