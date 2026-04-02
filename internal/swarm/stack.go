package swarm

import (
	"context"
	"fmt"

	"github.com/SomeBlackMagic/stackman/internal/compose"
)

type DeployOptions struct {
	PruneOrphans bool
	NoPull       bool
}

type DeploymentResult struct {
	DeployID        string
	UpdatedServices []ServiceUpdateResult
}

type StackDeployer struct {
	client    DockerClient
	stackName string
}

func NewStackDeployer(client DockerClient, stackName string) *StackDeployer {
	return &StackDeployer{
		client:    client,
		stackName: stackName,
	}
}

// Deploy executes a full stack deployment in deterministic step order.
func (d *StackDeployer) Deploy(ctx context.Context, cf *compose.ComposeFile, deployID string, opts DeployOptions) (*DeploymentResult, error) {
	if cf == nil {
		return nil, fmt.Errorf("deploy stack %q: nil compose file", d.stackName)
	}

	if err := RemoveExitedContainers(ctx, d.client, d.stackName); err != nil {
		return nil, fmt.Errorf("remove exited containers: %w", err)
	}

	if opts.PruneOrphans {
		desiredServices := make(map[string]bool, len(cf.Services))
		for name := range cf.Services {
			desiredServices[name] = true
		}
		if err := PruneOrphanedServices(ctx, d.client, d.stackName, desiredServices); err != nil {
			return nil, fmt.Errorf("prune orphaned services: %w", err)
		}

		desiredNetworks := make(map[string]bool, len(cf.Networks))
		for name := range cf.Networks {
			desiredNetworks[name] = true
		}
		if err := PruneOrphanedNetworks(ctx, d.client, d.stackName, desiredNetworks); err != nil {
			return nil, fmt.Errorf("prune orphaned networks: %w", err)
		}
	}

	if !opts.NoPull {
		if err := PullImages(ctx, d.client, cf.Services); err != nil {
			return nil, fmt.Errorf("pull images: %w", err)
		}
	}

	if err := EnsureNetworks(ctx, d.client, d.stackName, cf.Networks); err != nil {
		return nil, fmt.Errorf("ensure networks: %w", err)
	}

	if err := EnsureVolumes(ctx, d.client, d.stackName, cf.Volumes); err != nil {
		return nil, fmt.Errorf("ensure volumes: %w", err)
	}

	results, err := DeployServices(ctx, d.client, d.stackName, cf.Services, deployID)
	if err != nil {
		return nil, fmt.Errorf("deploy services: %w", err)
	}

	return &DeploymentResult{
		DeployID:        deployID,
		UpdatedServices: results,
	}, nil
}

// GetState returns current stack state.
func (d *StackDeployer) GetState(ctx context.Context) (*StackState, error) {
	return GetStackState(ctx, d.client, d.stackName)
}

// StackName returns target stack name for this deployer.
func (d *StackDeployer) StackName() string {
	return d.stackName
}
