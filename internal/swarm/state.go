package swarm

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/SomeBlackMagic/stackman/internal/plan"
)

// GetCurrentState reads the current state of a stack from Swarm
func GetCurrentState(ctx context.Context, cli StateClient, stackName string) (*plan.CurrentState, error) {
	state := &plan.CurrentState{
		Services: make(map[string]swarm.Service),
		Networks: make(map[string]swarm.Network),
		Volumes:  make(map[string]struct{}),
		Configs:  make(map[string]swarm.Config),
		Secrets:  make(map[string]swarm.Secret),
	}

	stackLabel := fmt.Sprintf("com.docker.stack.namespace=%s", stackName)

	// Get services
	serviceFilters := filters.NewArgs()
	serviceFilters.Add("label", stackLabel)

	services, err := cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: serviceFilters,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	for _, svc := range services {
		// Extract service name without stack prefix
		serviceName := svc.Spec.Name
		if len(serviceName) > len(stackName)+1 && serviceName[:len(stackName)] == stackName {
			serviceName = serviceName[len(stackName)+1:]
		}
		state.Services[serviceName] = svc
	}

	// Get networks
	networkFilters := filters.NewArgs()
	networkFilters.Add("label", stackLabel)

	networks, err := cli.NetworkList(ctx, network.ListOptions{
		Filters: networkFilters,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	for _, net := range networks {
		// Extract network name without stack prefix
		networkName := net.Name
		if len(networkName) > len(stackName)+1 && networkName[:len(stackName)] == stackName {
			networkName = networkName[len(stackName)+1:]
		}

		// Convert network.Summary to swarm.Network
		// Need to do full inspect to get complete details
		netInspect, err := cli.NetworkInspect(ctx, net.ID, network.InspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to inspect network %s: %w", net.Name, err)
		}

		state.Networks[networkName] = swarm.Network{
			ID: netInspect.ID,
			Spec: swarm.NetworkSpec{
				Annotations: swarm.Annotations{
					Name:   netInspect.Name,
					Labels: netInspect.Labels,
				},
				DriverConfiguration: &swarm.Driver{
					Name:    netInspect.Driver,
					Options: netInspect.Options,
				},
				IPAMOptions: convertIPAMConfig(&netInspect.IPAM),
			},
		}
	}

	// Get volumes
	volumeFilters := filters.NewArgs()
	volumeFilters.Add("label", stackLabel)

	volumes, err := cli.VolumeList(ctx, volume.ListOptions{
		Filters: volumeFilters,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	for _, vol := range volumes.Volumes {
		// Extract volume name without stack prefix
		volumeName := vol.Name
		if len(volumeName) > len(stackName)+1 && volumeName[:len(stackName)] == stackName {
			volumeName = volumeName[len(stackName)+1:]
		}
		state.Volumes[volumeName] = struct{}{}
	}

	// TODO: Get configs when configs support is added
	// TODO: Get secrets when secrets support is added

	return state, nil
}

// convertIPAMConfig converts network IPAM to swarm IPAMOptions
func convertIPAMConfig(ipam *network.IPAM) *swarm.IPAMOptions {
	if ipam == nil {
		return nil
	}

	var configs []swarm.IPAMConfig
	for _, cfg := range ipam.Config {
		configs = append(configs, swarm.IPAMConfig{
			Subnet:  cfg.Subnet,
			Gateway: cfg.Gateway,
		})
	}

	return &swarm.IPAMOptions{
		Driver: swarm.Driver{
			Name: ipam.Driver,
		},
		Configs: configs,
	}
}
