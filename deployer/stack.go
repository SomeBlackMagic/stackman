package deployer

import (
	"context"
	"fmt"
	"log"

	"github.com/docker/docker/api/types/swarm"

	"stackman/internal/compose"
)

type StackDeployer struct {
	cli                DockerClient
	stackName          string
	MaxFailedTaskCount int // Maximum number of failed tasks before giving up
}

// ServiceUpdateResult contains information about a service deployment
type ServiceUpdateResult struct {
	ServiceID   string        // Docker service ID
	ServiceName string        // Full service name (stack_service)
	Version     swarm.Version // Service version after update
	Warnings    []string      // Any warnings from Docker API
	Changed     bool          // Whether service was actually changed
}

// DeploymentResult contains information about all services deployed
type DeploymentResult struct {
	UpdatedServices []ServiceUpdateResult // Services that were created or updated
}

func NewStackDeployer(cli DockerClient, stackName string, maxFailedTaskCount int) *StackDeployer {
	if maxFailedTaskCount <= 0 {
		maxFailedTaskCount = 3 // Default value
	}
	return &StackDeployer{
		cli:                cli,
		stackName:          stackName,
		MaxFailedTaskCount: maxFailedTaskCount,
	}
}

// Deploy deploys a complete stack from a compose file
// Returns DeploymentResult with information about updated services, or error
func (d *StackDeployer) Deploy(ctx context.Context, composeFile *compose.ComposeFile) (*DeploymentResult, error) {
	// 1. Pull images
	if err := d.pullImages(ctx, composeFile.Services); err != nil {
		return nil, fmt.Errorf("failed to pull images: %w", err)
	}

	log.Printf("Starting deployment of stack: %s", d.stackName)

	// 2. Remove exited containers from previous deployments
	if err := d.RemoveExitedContainers(ctx); err != nil {
		return nil, fmt.Errorf("failed to remove exited containers: %w", err)
	}

	// 3. Check for obsolete services and remove them
	if err := d.removeObsoleteServices(ctx, composeFile.Services); err != nil {
		return nil, fmt.Errorf("failed to remove obsolete services: %w", err)
	}

	// 4. Create networks
	if err := d.createNetworks(ctx, composeFile.Networks); err != nil {
		return nil, fmt.Errorf("failed to create networks: %w", err)
	}

	// 5. Create volumes
	if err := d.createVolumes(ctx, composeFile.Volumes); err != nil {
		return nil, fmt.Errorf("failed to create volumes: %w", err)
	}

	// 6. Create/update services and collect results
	result, err := d.deployServices(ctx, composeFile.Services)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy services: %w", err)
	}

	log.Printf("Stack %s deployed successfully", d.stackName)
	return result, nil
}
