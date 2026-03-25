package registry

import (
	"container/list"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"codeberg.org/miekg/dns/dnsutil"
	"github.com/containers/podman/v6/pkg/bindings"
)

type ServiceRegistry struct {
	dnsMap    map[string]*Service // host -> service
	ipMap     map[string]*Service // ip -> service
	lruList   *list.List
	lruLookup map[*Service]*list.Element
	lruMu     sync.Mutex
	conn      context.Context
}

func ensureIP(ifaceName, targetIP string) error {
	// 1. Get the interface by name (e.g., "eth0")
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("could not find interface %s: %v", ifaceName, err)
	}
	// 2. Get all addresses currently assigned to it
	addrs, err := iface.Addrs()
	if err != nil {
		return err
	}
	// 3. Check if our target IP is already in the list
	for _, addr := range addrs {
		// addr.String() usually looks like "10.88.0.11/32"
		if strings.HasPrefix(addr.String(), targetIP) {
			fmt.Printf("[Network] IP %s is already set on %s. Skipping.\n", targetIP, ifaceName)
			return nil
		}
	}
	// 4. If not found, add it
	fmt.Printf("[Network] Adding IP %s to %s...\n", targetIP, ifaceName)
	cmd := exec.Command("ip", "addr", "add", targetIP+"/32", "dev", ifaceName)
	return cmd.Run()
}

func NewRegistry() (*ServiceRegistry, error) {
	conn, err := bindings.NewConnection(context.Background(), "unix:///run/podman/podman.sock")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	reg := &ServiceRegistry{
		dnsMap:    make(map[string]*Service),
		ipMap:     make(map[string]*Service),
		lruList:   list.New(),
		lruLookup: map[*Service]*list.Element{},
		lruMu:     sync.Mutex{},
		conn:      conn,
	}
	cfg := loadConfig("services.yml")
	for i := range cfg.Services {
		service := &cfg.Services[i]
		service.state = Unknown
		service.containerIp = ""
		service.activeCount = 0
		service.lastActive = time.UnixMilli(0)
		service.mu = sync.Mutex{}
		reg.updateContainerState(service)

		ensureIP("eth0", service.ProxyIp)

		reg.dnsMap[dnsutil.Fqdn(service.Host)] = service
		reg.ipMap[service.ProxyIp] = service
		element := reg.lruList.PushBack(service)
		reg.lruLookup[service] = element
	}
	return reg, nil
}

func (r *ServiceRegistry) Start() {
	go r.startJanitor()
	go r.startDNS()
	r.startProxy()
}
