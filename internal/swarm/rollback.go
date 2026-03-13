package swarm

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

// ServiceSnapshot stores the state of a service before deployment
type ServiceSnapshot struct {
	Service swarm.Service
	Tasks   []swarm.Task
}

// ResourceSnapshot stores state of stack resources
type ResourceSnapshot struct {
	Networks map[string]string // name -> ID
	Volumes  map[string]string // name -> ID
	// TODO: Add Configs and Secrets when implemented
}

// StackSnapshot stores the complete state of a stack before deployment
type StackSnapshot struct {
	StackName     string
	CreatedAt     time.Time
	Services      map[string]ServiceSnapshot // key is service ID
	ExistingIDs   map[string]bool            // IDs that existed before deploy
	Resources     ResourceSnapshot
	IsFirstDeploy bool // true if stack didn't exist before
}

// CreateSnapshot creates a snapshot of the current stack state
func (d *StackDeployer) CreateSnapshot(ctx context.Context) (*StackSnapshot, error) {
	d.logf("Creating snapshot of stack: %s", d.stackName)

	services, err := d.GetStackServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	snapshot := &StackSnapshot{
		StackName:     d.stackName,
		CreatedAt:     time.Now(),
		Services:      make(map[string]ServiceSnapshot),
		ExistingIDs:   make(map[string]bool),
		IsFirstDeploy: len(services) == 0,
		Resources: ResourceSnapshot{
			Networks: make(map[string]string),
			Volumes:  make(map[string]string),
		},
	}

	// Record existing service IDs
	for _, svc := range services {
		snapshot.ExistingIDs[svc.ID] = true
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
				d.logf("failed to list tasks for service %s (attempt %d/%d): %v, retrying in %v",
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
		// d.logf("Snapshotted service: %s (version %d)", svc.Spec.Name, svc.Version.Index)
	}

	d.logf("Snapshot created with %d services", len(snapshot.Services))
	return snapshot, nil
}

// Rollback restores the stack to a previous snapshot
func (d *StackDeployer) Rollback(ctx context.Context, snapshot *StackSnapshot) error {
	d.logf("Rolling back stack: %s", d.stackName)

	if snapshot == nil {
		d.logf("No snapshot available, skipping rollback")
		return nil
	}

	// If this was first deploy, remove all services
	if snapshot.IsFirstDeploy {
		d.logf("This was first deploy, removing all services")
		return d.removeAllServices(ctx)
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

	// Step 1: Remove new services that didn't exist in snapshot
	for _, svc := range currentServices {
		if !snapshot.ExistingIDs[svc.ID] {
			d.logf("Removing new service: %s (created during failed deploy)", svc.Spec.Name)
			if err := d.cli.ServiceRemove(ctx, svc.ID); err != nil {
				d.logf("Warning: failed to remove service %s: %v", svc.Spec.Name, err)
			} else {
				d.logf("Successfully removed service: %s", svc.Spec.Name)
			}
		}
	}

	// Step 2: Restore existing services from snapshot
	updatedServices := []string{}
	for serviceID, snap := range snapshot.Services {
		serviceName := snap.Service.Spec.Name

		// Check if service still exists
		current, exists := currentByID[serviceID]
		if !exists {
			d.logf("Service %s no longer exists, skipping rollback", serviceName)
			continue
		}

		d.logf("Rolling back service: %s to version %d", serviceName, snap.Service.Version.Index)

		// Restore service spec from snapshot
		rollbackSpec := snap.Service.Spec

		// Ensure update config for start-first behavior (seamless rollback)
		if rollbackSpec.UpdateConfig == nil {
			rollbackSpec.UpdateConfig = &swarm.UpdateConfig{}
		}
		// Set start-first order: new container starts before old one stops
		rollbackSpec.UpdateConfig.Order = swarm.UpdateOrderStartFirst
		// Set failure action to pause (safer for rollback)
		rollbackSpec.UpdateConfig.FailureAction = swarm.UpdateFailureActionPause

		// If update is paused, log it
		if current.UpdateStatus != nil && current.UpdateStatus.State == swarm.UpdateStatePaused {
			d.logf("Service %s update is paused, will be cleared by rollback update", serviceName)
		}

		// Update service to previous spec from snapshot
		_, err := d.cli.ServiceUpdate(
			ctx,
			serviceID,
			current.Version,
			rollbackSpec,
			types.ServiceUpdateOptions{
				RegistryAuthFrom: types.RegistryAuthFromPreviousSpec,
			},
		)

		if err != nil {
			d.logf("Failed to rollback service %s: %v", serviceName, err)
			return fmt.Errorf("rollback failed for service %s: %w", serviceName, err)
		}

		d.logf("Service %s rolled back successfully", serviceName)
		updatedServices = append(updatedServices, serviceName)
	}

	d.logf("Rollback completed for stack: %s (%d services restored)", d.stackName, len(updatedServices))
	return nil
}

// removeAllServices removes all services in the stack and waits for completion
func (d *StackDeployer) removeAllServices(ctx context.Context) error {
	services, err := d.GetStackServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	if len(services) == 0 {
		d.logln("No services to remove")
		return nil
	}

	// Track service IDs being removed
	removingIDs := make(map[string]string) // ID -> Name
	for _, svc := range services {
		removingIDs[svc.ID] = svc.Spec.Name
	}

	// Remove all services
	for _, svc := range services {
		d.logf("Removing service: %s", svc.Spec.Name)
		if err := d.cli.ServiceRemove(ctx, svc.ID); err != nil {
			d.logf("Warning: failed to remove service %s: %v", svc.Spec.Name, err)
			delete(removingIDs, svc.ID) // Don't wait for failed removals
		} else {
			d.logf("Initiated removal of service: %s", svc.Spec.Name)
		}
	}

	if len(removingIDs) == 0 {
		return nil
	}

	// Wait for services to be actually removed (poll until they're gone)
	d.logf("Waiting for %d service(s) to be removed...", len(removingIDs))
	maxWait := 30 * time.Second
	pollInterval := 500 * time.Millisecond
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		// Check each service individually via ServiceInspect
		for id, name := range removingIDs {
			_, _, err := d.cli.ServiceInspectWithRaw(ctx, id, types.ServiceInspectOptions{})
			if err != nil {
				// Service not found = successfully removed
				if client.IsErrNotFound(err) {
					d.logf("✓ Service removed: %s", name)
					delete(removingIDs, id)
				} else {
					d.logf("Warning: error inspecting service %s: %v", name, err)
				}
			}
			// If no error, service still exists, keep waiting
		}

		// All services removed?
		if len(removingIDs) == 0 {
			d.logln("All services successfully removed")
			return nil
		}

		time.Sleep(pollInterval)
	}

	// Timeout reached, report remaining services
	if len(removingIDs) > 0 {
		d.logf("Warning: %d service(s) still removing after %v:", len(removingIDs), maxWait)
		for _, name := range removingIDs {
			d.logf("  - %s", name)
		}
	}

	return nil
}
