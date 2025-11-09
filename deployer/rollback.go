package deployer

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
)

// ServiceSnapshot stores the state of a service before deployment
type ServiceSnapshot struct {
	Service swarm.Service
	Tasks   []swarm.Task
}

// StackSnapshot stores the complete state of a stack before deployment
type StackSnapshot struct {
	Services map[string]ServiceSnapshot // key is service ID
}

// CreateSnapshot creates a snapshot of the current stack state
func (d *StackDeployer) CreateSnapshot(ctx context.Context) (*StackSnapshot, error) {
	log.Printf("Creating snapshot of stack: %s", d.stackName)

	services, err := d.GetStackServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	snapshot := &StackSnapshot{
		Services: make(map[string]ServiceSnapshot),
	}

	for _, svc := range services {
		// Get tasks for this service
		// Retry with exponential backoff for API timeouts
		var tasks []swarm.Task
		var err error
		maxRetries := 3
		for retry := 0; retry < maxRetries; retry++ {
			tasks, err = d.cli.TaskList(ctx, types.TaskListOptions{
				Filters: filters.NewArgs(
					filters.Arg("service", svc.ID),
				),
			})
			if err == nil {
				break
			}
			if retry < maxRetries-1 {
				waitTime := time.Duration(retry+1) * time.Second
				log.Printf("failed to list tasks for service %s (attempt %d/%d): %v, retrying in %v",
					svc.Spec.Name, retry+1, maxRetries, err, waitTime)
				time.Sleep(waitTime)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list tasks for service %s: %w", svc.Spec.Name, err)
		}

		snapshot.Services[svc.ID] = ServiceSnapshot{
			Service: svc,
			Tasks:   tasks,
		}
		// TODO Debug log
		// log.Printf("Snapshotted service: %s (version %d)", svc.Spec.Name, svc.Version.Index)
	}

	log.Printf("Snapshot created with %d services", len(snapshot.Services))
	return snapshot, nil
}

// Rollback restores the stack to a previous snapshot
func (d *StackDeployer) Rollback(ctx context.Context, snapshot *StackSnapshot) error {
	log.Printf("Rolling back stack: %s", d.stackName)

	if snapshot == nil || len(snapshot.Services) == 0 {
		log.Printf("No snapshot available, skipping rollback")
		return nil
	}

	// Get current services
	currentServices, err := d.GetStackServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to list current services: %w", err)
	}

	// Build map of current services by ID
	currentByID := make(map[string]swarm.Service)
	for _, svc := range currentServices {
		currentByID[svc.ID] = svc
	}

	// Restore each service from snapshot
	for serviceID, snap := range snapshot.Services {
		serviceName := snap.Service.Spec.Name

		// Check if service still exists
		current, exists := currentByID[serviceID]
		if !exists {
			log.Printf("Service %s no longer exists, skipping rollback", serviceName)
			continue
		}

		log.Printf("Rolling back service: %s to version %d", serviceName, snap.Service.Version.Index)

		// Update service to previous spec
		_, err := d.cli.ServiceUpdate(
			ctx,
			serviceID,
			current.Version, // Use current version for optimistic locking
			snap.Service.Spec,
			types.ServiceUpdateOptions{
				// Use rollback mode
				RegistryAuthFrom: types.RegistryAuthFromPreviousSpec,
			},
		)
		if err != nil {
			log.Printf("Failed to rollback service %s: %v", serviceName, err)
			continue
		}

		log.Printf("Service %s rolled back successfully", serviceName)
	}

	log.Printf("Rollback completed for stack: %s", d.stackName)
	return nil
}
