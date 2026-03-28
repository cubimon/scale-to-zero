package registry

import (
	"container/list"
	"context"
	"encoding/binary"
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
	cfg       Config
	dnsMap    map[string]*Service // host -> service
	ipMap     map[string]*Service // ip -> service
	lruList   *list.List
	lruLookup map[*Service]*list.Element
	lruMu     sync.Mutex
	conn      context.Context
}

func isAddrIPv4(addr net.Addr) bool {
	switch v := addr.(type) {
	case *net.TCPAddr:
		return v.IP.To4() != nil
	case *net.UDPAddr:
		return v.IP.To4() != nil
	case *net.IPAddr:
		return v.IP.To4() != nil
	}
	return false
}

func getIPv4(addr net.Addr) net.IP {
	var ip net.IP
	switch v := addr.(type) {
	case *net.TCPAddr:
		ip = v.IP
	case *net.UDPAddr:
		ip = v.IP
	case *net.IPAddr:
		ip = v.IP
	default:
		return nil
	}
	return ip.To4()
}

func intToIP4(ipInt uint32) string {
	ipBytes := make(net.IP, 4)
	binary.BigEndian.PutUint32(ipBytes, ipInt)
	return ipBytes.String()
}

func flushIPRange(ifaceName, rangeStart, rangeEnd string) error {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("Could not find interface %s: %v", ifaceName, err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		fmt.Println("Failed to get interface ip addresses")
		return err
	}
	rangeStartInt := binary.BigEndian.Uint32(net.ParseIP(rangeStart).To4())
	rangeEndInt := binary.BigEndian.Uint32(net.ParseIP(rangeStart).To4())
	for _, addr := range addrs {
		if !isAddrIPv4(addr) {
			continue
		}
		addrIP4 := getIPv4(addr)
		addrInt := binary.BigEndian.Uint32(addrIP4)
		if rangeStartInt <= addrInt && addrInt <= rangeEndInt {
			fmt.Println("Removing ip address", addrIP4)
			cmd := exec.Command("ip", "addr", "del", addrIP4.String()+"/32", "dev", ifaceName)
			err := cmd.Run()
			if err != nil {
				fmt.Println("Failed to remove ip address")
				return err
			}
		}
	}
	return nil
}

func ensureIP(ifaceName, targetIP string) error {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("could not find interface %s: %v", ifaceName, err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return err
	}
	for _, addr := range addrs {
		if strings.HasPrefix(addr.String(), targetIP) {
			fmt.Printf("[Network] IP %s is already set on %s. Skipping.\n", targetIP, ifaceName)
			return nil
		}
	}
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
	reg.cfg = cfg
	flushIPRange(cfg.Iface, cfg.IPAM.Start, cfg.IPAM.End)
	ipamNext := binary.BigEndian.Uint32(
		net.ParseIP(cfg.IPAM.Start).To4())
	ipamLast := binary.BigEndian.Uint32(
		net.ParseIP(cfg.IPAM.End).To4())
	for i := range cfg.Services {
		service := &cfg.Services[i]
		service.state = Unknown
		if ipamNext > ipamLast {
			panic("Not enough ip addresses")
		}
		service.proxyIp = intToIP4(ipamNext)
		ipamNext++
		service.containerIp = ""
		service.activeCount = 0
		service.lastActive = time.UnixMilli(0)
		service.mu = sync.Mutex{}
		reg.updateContainerState(service)

		ensureIP(cfg.Iface, service.proxyIp)
		if ipamNext > ipamLast {
			panic("Not enough ip addresses")
		}
		reg.createContainerUnsafe(
			service,
			intToIP4(ipamNext),
			cfg.IP)
		ipamNext++

		reg.dnsMap[dnsutil.Fqdn(service.Host)] = service
		reg.ipMap[service.proxyIp] = service
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
