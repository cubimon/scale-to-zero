package registry

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
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

func (r *ServiceRegistry) evictLru() {
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
			service.mu.Unlock()
			return
		}
		service.mu.Unlock()
	}
}

func (r *ServiceRegistry) startTimeoutJanitor() {
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

func getAvailableMem() (int64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemAvailable:") {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return 0, fmt.Errorf("invalid format")
			}
			return strconv.ParseInt(parts[1], 10, 64)
		}
	}
	return 0, fmt.Errorf("MemAvailable not found")
}

func (r *ServiceRegistry) startPressureJanitor() {
	// PSI (Pressure Stall Information) Trigger
	// Format: <some|full> <stall_duration_us> <window_us>
	// This triggers if "some" tasks stall for 50ms (50000us) in a 1s (1000000us) window.
	defaultPressureTrigger := "some 50000 1000000"
	defaultMemoryThreshold := 2048
	pressureTrigger := defaultPressureTrigger
	memoryThreshold := defaultMemoryThreshold
	if r.cfg.Janitor.PressureTrigger != "" {
		pressureTrigger = r.cfg.Janitor.PressureTrigger
	}
	if r.cfg.Janitor.MemoryThreshold != 0 {
		memoryThreshold = r.cfg.Janitor.MemoryThreshold
	}
	f, err := os.OpenFile("/proc/pressure/memory", os.O_WRONLY, 1)
	if err != nil {
		log.Fatalf("Failed to open PSI: %v (Is PSI enabled in kernel?)", err)
	}
	defer f.Close()
	if _, err := f.WriteString(pressureTrigger); err != nil {
		log.Fatalf("Failed to write PSI trigger: %v", err)
	}
	// Epoll to wait for the event without polling
	epfd, _ := unix.EpollCreate1(0)
	event := unix.EpollEvent{
		Events: unix.EPOLLPRI,
		Fd:     int32(f.Fd()),
	}
	unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, int(f.Fd()), &event)
	log.Println("Monitoring started. Waiting for memory pressure...")
	events := make([]unix.EpollEvent, 1)
	for {
		// Wait for pressure event (blocks here indefinitely)
		n, err := unix.EpollWait(epfd, events, -1)
		log.Printf("Epoll wait event: %d", n)
		if err != nil && err != syscall.EINTR {
			log.Printf("Epoll error: %v", err)
			continue
		}
		if n > 0 {
			// Double-check raw memory to see if we should actually kill things
			avail, _ := getAvailableMem()
			fmt.Printf("Pressure Event! Available: %d MB\n", avail/1024)
			if avail < int64(memoryThreshold)*1024 {
				fmt.Println("Evict based on LRU, too little memory available")
				r.evictLru()
				// Sleep to avoid "flapping" (repeatedly triggering)
				time.Sleep(30 * time.Second)
			}
		}
	}
}
