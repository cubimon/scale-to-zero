package registry

import (
	"fmt"
	"net"
	"time"

	"github.com/containers/podman/v6/pkg/bindings/containers"
	"github.com/containers/podman/v6/pkg/specgen"
	nettypes "go.podman.io/common/libnetwork/types"
)

func (sc *Service) containerName() string {
	return sc.Host + "-container"
}

func (r *ServiceRegistry) updateContainerState(service *Service) error {
	service.mu.Lock()
	err := r.updateContainerStateUnsafe(service)
	service.mu.Unlock()
	return err
}

func (r *ServiceRegistry) updateContainerStateUnsafe(service *Service) error {
	containerExists, err := containers.Exists(r.conn, service.containerName(), nil)
	if err != nil {
		fmt.Println("Failed to check if container exists ", service.containerName())
		return err
	}
	if !containerExists {
		return nil
	}
	report, err := containers.Inspect(r.conn, service.containerName(), nil)
	if err != nil {
		fmt.Println("Failed to inspect container ", service.containerName())
		return err
	}
	if report.State.Running && service.state != StateStarted {
		service.state = StateStarted
	}
	if report.State.Checkpointed && service.state != StateStopped {
		service.state = StateStopped
	}
	for _, net := range report.NetworkSettings.Networks {
		if net.IPAddress != "" {
			fmt.Println("container ip address", net.IPAddress)
			service.containerIp = net.IPAddress
		}
	}
	return nil
}

func (r *ServiceRegistry) waitContainerStarted(service *Service) error {
	err := retry(r.conn, 10, 500*time.Millisecond, func() error {
		report, err := containers.Inspect(r.conn, service.containerName(), nil)
		if err != nil {
			return err
		}
		if !report.State.Running {
			return fmt.Errorf("Container is %s, not running", report.State.Status)
		}
		for _, net := range report.NetworkSettings.Networks {
			if net.IPAddress != "" {
				return nil
			}
		}
		return fmt.Errorf("Container is still missing an ip address")
	})
	r.updateContainerState(service)
	return err
}

func (r *ServiceRegistry) waitContainerStopped(service *Service) error {
	err := retry(r.conn, 10, 500*time.Millisecond, func() error {
		report, err := containers.Inspect(r.conn, service.containerName(), nil)
		if err != nil {
			return err
		}
		if !report.State.Checkpointed {
			return fmt.Errorf("Container is %s, not exited", report.State.Status)
		}
		return nil
	})
	r.updateContainerState(service)
	return err
}

func (r *ServiceRegistry) startContainer(service *Service) error {
	service.mu.Lock()
	err := r.startContainerUnsafe(service)
	service.mu.Unlock()
	return err
}

func (r *ServiceRegistry) startContainerUnsafe(service *Service) error {
	if service.state == StateStarted || service.state == StateStarting {
		return nil
	}
	if service.state == StateStopping {
		return fmt.Errorf("Can't start container that is still stopping")
	}
	// service.state == StateStopped || service.state == Unknown
	fmt.Println("Starting ", service.Host)
	report, _ := containers.Inspect(r.conn, service.containerName(), nil)
	if report.State.Checkpointed {
		fmt.Println("Restoring container ", service.containerName())
		// restore with tcp, since we checkpoint with tcp
		opts := new(containers.RestoreOptions).
			WithTCPEstablished(true)
		containers.Restore(r.conn, service.containerName(), opts)
	} else {
		fmt.Println("Starting container ", service.containerName())
		containers.Start(r.conn, service.containerName(), nil)
	}
	service.state = StateStarting
	return nil
}

func (r *ServiceRegistry) createContainerUnsafe(service *Service, ipAddress string) error {
	containerExists, _ := containers.Exists(r.conn, service.containerName(), nil)
	if containerExists {
		return nil
	}
	spec := specgen.NewSpecGenerator(service.Image, false)
	spec.Name = service.containerName()
	spec.Networks = map[string]nettypes.PerNetworkOptions{
		"internal-proxy-net": {
			StaticIPs: []net.IP{
				net.ParseIP(ipAddress),
			},
		},
	}
	spec.NetNS = specgen.Namespace{
		NSMode: specgen.Bridge,
	}
	_, err := containers.CreateWithSpec(r.conn, spec, nil)
	if err != nil {
		// If it exists, we just want to start it.
		// In production, check if the error is "already exists"
		fmt.Printf("Container creation skipped/failed: %v\n", err)
	}
	service.state = StateStopped
	return err
}

func (r *ServiceRegistry) stopContainer(service *Service) error {
	service.mu.Lock()
	err := r.stopContainerUnsafe(service)
	service.mu.Unlock()
	return err
}

func (r *ServiceRegistry) stopContainerUnsafe(service *Service) error {
	if service.state == Unknown {
		err := r.updateContainerStateUnsafe(service)
		if err != nil {
			return err
		}
	}
	if service.state == StateStopping || service.state == StateStopped {
		return nil
	}
	if service.state == StateStarting {
		return fmt.Errorf("Can't suspend container that is still starting")
	}
	// service.state == StateStarted
	fmt.Println("Stopping container ", service.containerName())
	opts := new(containers.CheckpointOptions).
		WithTCPEstablished(true). // Essential for apps with open sockets
		WithIgnoreRootfs(true).   // Speeds up process by not saving file changes
		WithKeep(false)           // Delete temporary CRIU files after success
	_, err := containers.Checkpoint(r.conn, service.containerName(), opts)
	if err != nil {
		fmt.Println("Stopping container failed", service.containerName(), err)
	} else {
		service.state = StateStopping
		fmt.Println("Stopping container succeeded", service.containerName())
	}
	return err
}
