package deployer

import (
	"context"
	"fmt"
	"log"

	"github.com/docker/docker/api/types/network"

	"stackman/compose"
)

func (d *StackDeployer) createNetworks(ctx context.Context, networks map[string]*compose.Network) error {
	if len(networks) == 0 {
		// Create default network for the stack
		return d.ensureDefaultNetwork(ctx)
	}

	for name, netConfig := range networks {
		fullName := fmt.Sprintf("%s_%s", d.stackName, name)

		// Check if network already exists
		_, err := d.cli.NetworkInspect(ctx, fullName, network.InspectOptions{})
		if err == nil {
			log.Printf("Network %s already exists", fullName)
			continue
		}

		// Create network
		driver := "overlay"
		if netConfig != nil && netConfig.Driver != "" {
			driver = netConfig.Driver
		}

		labels := map[string]string{
			"com.docker.stack.namespace": d.stackName,
		}
		if netConfig != nil && netConfig.Labels != nil {
			for k, v := range netConfig.Labels {
				labels[k] = v
			}
		}

		opts := network.CreateOptions{
			Driver:     driver,
			Labels:     labels,
			Attachable: netConfig != nil && netConfig.Attachable,
		}

		if netConfig != nil && netConfig.DriverOpts != nil {
			opts.Options = netConfig.DriverOpts
		}

		_, err = d.cli.NetworkCreate(ctx, fullName, opts)
		if err != nil {
			return fmt.Errorf("failed to create network %s: %w", fullName, err)
		}

		log.Printf("Created network: %s", fullName)
	}

	return nil
}

func (d *StackDeployer) ensureDefaultNetwork(ctx context.Context) error {
	networkName := fmt.Sprintf("%s_default", d.stackName)

	// Check if network exists
	_, err := d.cli.NetworkInspect(ctx, networkName, network.InspectOptions{})
	if err == nil {
		log.Printf("Default network %s already exists", networkName)
		return nil
	}

	// Create default overlay network
	_, err = d.cli.NetworkCreate(ctx, networkName, network.CreateOptions{
		Driver: "overlay",
		Labels: map[string]string{
			"com.docker.stack.namespace": d.stackName,
		},
		Attachable: true,
	})

	if err != nil {
		return fmt.Errorf("failed to create default network: %w", err)
	}

	log.Printf("Created default network: %s", networkName)
	return nil
}
