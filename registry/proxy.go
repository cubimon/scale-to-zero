package registry

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func (sc *Service) remoteAddress() string {
	remoteIP := sc.RemoteIP
	if sc.isContainerService() {
		remoteIP = sc.containerIP
	}
	return fmt.Sprintf("%s:%d", remoteIP, sc.RemotePort)
}

func (r *ServiceRegistry) startContainerAndWait(service *Service) {
	if !service.isContainerService() {
		return
	}
	service.mu.Lock()
	if service.state == StateStarted {
		service.mu.Unlock()
		return
	}
	if service.state == StateStopping {
		service.mu.Unlock()
		err := r.waitContainerStopped(service)
		if err != nil {
			fmt.Println("Failed to wait for container to be stopped ", service.Host)
			return
		}
	} else {
		service.mu.Unlock()
	}
	// unknown/stopped
	err := r.startContainer(service)
	if err != nil {
		fmt.Println("Failed to start container ", service.Host)
		return
	}
	// state = StateStarting
	err = r.waitContainerStarted(service)
	if err != nil {
		fmt.Println("Failed to wait for container to be started ", service.Host)
		return
	}
}

func (r *ServiceRegistry) waitForPort(containerAddress string) error {
	err := retry(r.conn, 60, 1000*time.Millisecond, func() error {
		conn, err := net.DialTimeout("tcp", containerAddress, 200*time.Millisecond)
		if err != nil {
			return err
		}
		fmt.Println("tcp port is open", containerAddress)
		conn.Close()
		return nil
	})
	if err != nil {
		fmt.Println("container never opened up tcp port: %w", err)
		return err
	}
	return nil
}

func (r *ServiceRegistry) startProxy() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		localAddr, _ := req.Context().Value(http.LocalAddrContextKey).(net.Addr)
		ip, _, _ := net.SplitHostPort(localAddr.String())
		fmt.Println("Request going to addr", ip)
		service := r.ipMap[ip]
		r.markActive(service)
		r.startContainerAndWait(service)
		remoteAddress := service.remoteAddress()
		target, _ := url.Parse("http://" + remoteAddress)
		proxy := httputil.NewSingleHostReverseProxy(target)
		service.mu.Lock()
		service.activeCount += 1
		service.mu.Unlock()
		proxy.ServeHTTP(w, req)
		service.mu.Lock()
		service.activeCount -= 1
		service.mu.Unlock()
	})
	log.Fatal(http.ListenAndServe("0.0.0.0:80", handler))
}
