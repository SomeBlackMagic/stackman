package swarm

import (
	"context"
	"fmt"
	"strings"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
)

// RemoveExitedContainers removes stack containers in exited/dead states.
func RemoveExitedContainers(ctx context.Context, client DockerClient, stackName string) error {
	containers, err := client.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", stackNamespaceLabel+"="+stackName),
			filters.Arg("status", "exited"),
			filters.Arg("status", "dead"),
		),
	})
	if err != nil {
		return fmt.Errorf("list exited containers: %w", err)
	}

	for _, ctr := range containers {
		if err := client.ContainerRemove(ctx, ctr.ID, container.RemoveOptions{
			Force:         true,
			RemoveVolumes: false,
		}); err != nil {
			return fmt.Errorf("remove container %q: %w", ctr.ID, err)
		}
	}

	return nil
}

// PruneOrphanedServices removes stack services absent in desired.
// desired contains service names without "<stack>_" prefix.
func PruneOrphanedServices(ctx context.Context, client DockerClient, stackName string, desired map[string]bool) error {
	services, err := client.ServiceList(ctx, dockertypes.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("label", stackNamespaceLabel+"="+stackName)),
	})
	if err != nil {
		return fmt.Errorf("list services: %w", err)
	}

	prefix := stackName + "_"
	for _, svc := range services {
		name := strings.TrimPrefix(svc.Spec.Name, prefix)
		if desired[name] {
			continue
		}
		if err := client.ServiceRemove(ctx, svc.ID); err != nil {
			return fmt.Errorf("remove orphaned service %q: %w", svc.Spec.Name, err)
		}
	}

	return nil
}

// PruneOrphanedNetworks removes stack networks absent in desired.
// desired contains network names without "<stack>_" prefix.
func PruneOrphanedNetworks(ctx context.Context, client DockerClient, stackName string, desired map[string]bool) error {
	networks, err := client.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", stackNamespaceLabel+"="+stackName)),
	})
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}

	prefix := stackName + "_"
	for _, net := range networks {
		name := strings.TrimPrefix(net.Name, prefix)
		if desired[name] {
			continue
		}
		if err := client.NetworkRemove(ctx, net.ID); err != nil {
			return fmt.Errorf("remove orphaned network %q: %w", net.Name, err)
		}
	}

	return nil
}
