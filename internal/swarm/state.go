package swarm

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

const stackNamespaceLabel = "com.docker.stack.namespace"

// StackState contains the current swarm resources for a stack.
// Map keys are normalized names without "<stackName>_" prefix.
type StackState struct {
	Services map[string]swarm.Service
	Networks map[string]network.Summary
	Volumes  map[string]volume.Volume
}

// GetStackState fetches all stack resources filtered by stack namespace label.
// It returns an empty state when the stack does not exist.
func GetStackState(ctx context.Context, client DockerClient, stackName string) (*StackState, error) {
	state := &StackState{
		Services: make(map[string]swarm.Service),
		Networks: make(map[string]network.Summary),
		Volumes:  make(map[string]volume.Volume),
	}

	labelFilter := stackNamespaceLabel + "=" + stackName
	namePrefix := stackName + "_"

	services, err := client.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("label", labelFilter)),
	})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	for _, svc := range services {
		name := strings.TrimPrefix(svc.Spec.Name, namePrefix)
		state.Services[name] = svc
	}

	networks, err := client.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", labelFilter)),
	})
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	for _, net := range networks {
		name := strings.TrimPrefix(net.Name, namePrefix)
		state.Networks[name] = net
	}

	volumesResp, err := client.VolumeList(ctx, filters.NewArgs(filters.Arg("label", labelFilter)))
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	for _, vol := range volumesResp.Volumes {
		name := strings.TrimPrefix(vol.Name, namePrefix)
		state.Volumes[name] = *vol
	}

	return state, nil
}
