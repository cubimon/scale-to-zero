package registry

import (
	"context"
	"fmt"
	"time"
)

func (r *ServiceRegistry) markActive(service *Service) {
	r.lruMu.Lock()
	defer r.lruMu.Unlock()
	if element, ok := r.lruLookup[service]; ok {
		// Move to front: O(1)
		r.lruList.MoveToFront(element)
		// Update the timestamp inside the actual ServiceState struct
		service.mu.Lock()
		service.lastActive = time.Now()
		service.mu.Unlock()
	}
}

func (r *ServiceRegistry) evictLru(conn context.Context) {
	r.lruMu.Lock()
	defer r.lruMu.Unlock()

	// Start from the tail (Least Recently Used)
	for service := r.lruList.Back(); service != nil; service = service.Prev() {
		service := service.Value.(*Service)
		service.mu.Lock()
		if service.state == StateStarted && service.activeCount == 0 {
			fmt.Println("Evicting lru ", service.Host)
			r.stopContainerUnsafe(service)
			service.state = StateStopped
		}
		service.mu.Unlock()
	}
}

func (r *ServiceRegistry) startJanitor() {
	ticker := time.NewTicker(time.Duration(r.cfg.Janitor.PollInterval) * time.Second)
	defer ticker.Stop()
	idleTimeout := time.Duration(r.cfg.Janitor.Timeout) * time.Second

	for {
		select {
		case <-ticker.C:
			fmt.Println("Janitor tick now")
			r.lruMu.Lock()
			now := time.Now()
			for service := r.lruList.Back(); service != nil; service = service.Prev() {
				service := service.Value.(*Service)
				service.mu.Lock()
				if service.state == Unknown || service.state == StateStarting || service.state == StateStopping {
					// update state if we are in changing state
					r.updateContainerStateUnsafe(service)
				}
				if service.state == StateStarted &&
					service.activeCount == 0 &&
					now.Sub(service.lastActive) > idleTimeout {
					fmt.Printf("Service %s is idle. Stopping...\n", service.Host)
					r.stopContainerUnsafe(service)
				}
				service.mu.Unlock()
			}
			r.lruMu.Unlock()

		case <-r.conn.Done():
			return
		}
	}
}
