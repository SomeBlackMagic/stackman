package deployer

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"

	"stackman/compose"
)

func (d *StackDeployer) deployServices(ctx context.Context, services map[string]*compose.Service) (*DeploymentResult, error) {
	result := &DeploymentResult{
		UpdatedServices: make([]ServiceUpdateResult, 0, len(services)),
	}

	for name, svc := range services {
		updateResult, err := d.deployService(ctx, name, svc)
		if err != nil {
			return nil, fmt.Errorf("failed to deploy service %s: %w", name, err)
		}

		// Only add to results if service was actually changed
		if updateResult != nil && updateResult.Changed {
			result.UpdatedServices = append(result.UpdatedServices, *updateResult)
		}
	}

	return result, nil
}

func (d *StackDeployer) deployService(ctx context.Context, serviceName string, service *compose.Service) (*ServiceUpdateResult, error) {
	fullName := fmt.Sprintf("%s_%s", d.stackName, serviceName)

	// Convert compose service to swarm spec
	spec, err := compose.ConvertToSwarmSpec(serviceName, service, d.stackName)
	if err != nil {
		return nil, fmt.Errorf("failed to convert service spec: %w", err)
	}

	// Attach to default network if no networks specified
	if service.Networks == nil {
		defaultNetwork := fmt.Sprintf("%s_default", d.stackName)
		spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{
			{Target: defaultNetwork},
		}
	}

	// Get registry auth for the image
	registryAuth := getRegistryAuth(service.Image)

	// Check if service exists
	existingServices, err := d.cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(
			filters.Arg("name", fullName),
		),
		Status: true,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	if len(existingServices) > 0 {
		// Update existing service
		existing := existingServices[0]
		log.Printf("Updating service: %s", fullName)

		// Get current tasks before update to track recreation
		// Retry with exponential backoff for API timeouts
		var oldTasks []swarm.Task
		maxRetries := 3
		for retry := 0; retry < maxRetries; retry++ {
			oldTasks, err = d.cli.TaskList(ctx, types.TaskListOptions{
				Filters: filters.NewArgs(
					filters.Arg("service", existing.ID),
					filters.Arg("desired-state", "running"),
				),
			})
			if err == nil {
				break
			}
			if retry < maxRetries-1 {
				waitTime := time.Duration(retry+1) * time.Second
				log.Printf("failed to list old tasks (attempt %d/%d): %v, retrying in %v",
					retry+1, maxRetries, err, waitTime)
				time.Sleep(waitTime)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list old tasks: %w", err)
		}

		response, err := d.cli.ServiceUpdate(
			ctx,
			existing.ID,
			existing.Version,
			*spec,
			types.ServiceUpdateOptions{
				EncodedRegistryAuth: registryAuth,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to update service: %w", err)
		}

		// Wait a bit for Docker to process the update
		time.Sleep(1 * time.Second)

		// Check if tasks were actually recreated by comparing task IDs
		// Retry with exponential backoff for API timeouts
		var newTasks []swarm.Task
		for retry := 0; retry < maxRetries; retry++ {
			newTasks, err = d.cli.TaskList(ctx, types.TaskListOptions{
				Filters: filters.NewArgs(
					filters.Arg("service", existing.ID),
					filters.Arg("desired-state", "running"),
				),
			})
			if err == nil {
				break
			}
			if retry < maxRetries-1 {
				waitTime := time.Duration(retry+1) * time.Second
				log.Printf("failed to list new tasks (attempt %d/%d): %v, retrying in %v",
					retry+1, maxRetries, err, waitTime)
				time.Sleep(waitTime)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list new tasks: %w", err)
		}

		// Build map of old task IDs
		oldTaskIDs := make(map[string]bool)
		for _, task := range oldTasks {
			oldTaskIDs[task.ID] = true
		}

		// Check if any new task appeared
		hasNewTasks := false
		for _, task := range newTasks {
			if !oldTaskIDs[task.ID] {
				hasNewTasks = true
				break
			}
		}

		if hasNewTasks {
			log.Printf("Service %s updated, new tasks will be created", fullName)

			// Get updated service to retrieve new version
			updatedService, _, err := d.cli.ServiceInspectWithRaw(ctx, existing.ID, types.ServiceInspectOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to inspect updated service: %w", err)
			}

			// Return update result immediately - don't wait for tasks
			return &ServiceUpdateResult{
				ServiceID:   existing.ID,
				ServiceName: fullName,
				Version:     updatedService.Version,
				Warnings:    response.Warnings,
				Changed:     true,
			}, nil
		} else {
			log.Printf("Service %s: no changes detected (tasks not recreated)", fullName)

			// Return nil - service was NOT changed
			return nil, nil
		}
	} else {
		// Create new service
		log.Printf("Creating service: %s", fullName)

		createResponse, err := d.cli.ServiceCreate(ctx, *spec, types.ServiceCreateOptions{
			EncodedRegistryAuth: registryAuth,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create service: %w", err)
		}
		log.Printf("Service %s created", fullName)

		// Get created service to retrieve version
		createdService, _, err := d.cli.ServiceInspectWithRaw(ctx, createResponse.ID, types.ServiceInspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to inspect created service: %w", err)
		}

		// Return create result - service was created (changed)
		return &ServiceUpdateResult{
			ServiceID:   createResponse.ID,
			ServiceName: fullName,
			Version:     createdService.Version,
			Warnings:    createResponse.Warnings,
			Changed:     true,
		}, nil
	}
}

// GetStackServices returns all services in the stack
func (d *StackDeployer) GetStackServices(ctx context.Context) ([]swarm.Service, error) {
	return d.cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("com.docker.stack.namespace=%s", d.stackName)),
		),
		Status: true,
	})
}

// waitForServiceUpdate waits for service tasks to be recreated after update
func (d *StackDeployer) waitForServiceUpdate(ctx context.Context, serviceID string, oldTasks []swarm.Task) error {
	// Build map of old task IDs
	oldTaskIDs := make(map[string]bool)
	for _, task := range oldTasks {
		oldTaskIDs[task.ID] = true
	}

	log.Printf("Tracking %d old tasks for service update", len(oldTasks))

	start := time.Now()
	timeout := 5 * time.Minute
	seenFailedTasks := make(map[string]bool)
	newFailedTaskCount := 0
	lastStatusLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for service update")
		default:
		}

		// Check timeout
		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for service update after %v", timeout)
		}

		// Get current tasks
		// Retry with exponential backoff for API timeouts
		var currentTasks []swarm.Task
		var err error
		maxRetries := 3
		for retry := 0; retry < maxRetries; retry++ {
			currentTasks, err = d.cli.TaskList(ctx, types.TaskListOptions{
				Filters: filters.NewArgs(
					filters.Arg("service", serviceID),
				),
			})
			if err == nil {
				break
			}
			if retry < maxRetries-1 {
				waitTime := time.Duration(retry+1) * time.Second
				log.Printf("failed to list tasks (attempt %d/%d): %v, retrying in %v",
					retry+1, maxRetries, err, waitTime)
				time.Sleep(waitTime)
			}
		}
		if err != nil {
			return fmt.Errorf("failed to list tasks: %w", err)
		}

		// Log task summary every 10 seconds
		if time.Since(lastStatusLog) > 10*time.Second {
			var taskStates []string
			newTaskCount := 0
			oldTaskCount := 0
			for _, task := range currentTasks {
				if oldTaskIDs[task.ID] {
					oldTaskCount++
					taskStates = append(taskStates, fmt.Sprintf("OLD-%s:%s", task.ID[:12], task.Status.State))
				} else {
					newTaskCount++
					taskStates = append(taskStates, fmt.Sprintf("NEW-%s:%s", task.ID[:12], task.Status.State))
				}
			}
			// TODO migrate to debug
			// log.Printf("Task status: %d old, %d new. States: %v", oldTaskCount, newTaskCount, taskStates)
			lastStatusLog = time.Now()
		}

		// Check for failed new tasks
		for _, task := range currentTasks {
			// Skip old tasks
			if oldTaskIDs[task.ID] {
				continue
			}

			// Check if new task failed or completed abnormally
			// Skip tasks that completed successfully (exit code 0, no error)
			isFailed := task.Status.State == swarm.TaskStateFailed ||
				task.Status.State == swarm.TaskStateRejected ||
				(task.Status.State == swarm.TaskStateShutdown && task.Status.Err != "")

			// For complete state, only count as failed if there's an error or non-zero exit code
			if task.Status.State == swarm.TaskStateComplete && task.DesiredState == swarm.TaskStateShutdown {
				// This is a replaced task - only fail if there's evidence of actual failure
				if task.Status.Err != "" || task.Status.ContainerStatus.ExitCode != 0 {
					isFailed = true
				}
			}

			if isFailed {
				// Log each failed task once
				if !seenFailedTasks[task.ID] {
					seenFailedTasks[task.ID] = true
					newFailedTaskCount++

					if task.Status.Err != "" {
						log.Printf("ERROR: New task %s failed with state %s (desired: %s): %s",
							task.ID[:12], task.Status.State, task.DesiredState, task.Status.Err)
					} else {
						log.Printf("ERROR: New task %s failed with state %s (desired: %s)",
							task.ID[:12], task.Status.State, task.DesiredState)
					}

					if task.Status.ContainerStatus.ExitCode != 0 {
						log.Printf("  Container exit code: %d", task.Status.ContainerStatus.ExitCode)
					}
				}

				// If we have enough failed new tasks, give up
				if newFailedTaskCount >= d.MaxFailedTaskCount {
					return fmt.Errorf("service update failed: %d new tasks failed (healthcheck or startup failures)", newFailedTaskCount)
				}
			}
		}

		// Check if all old tasks are shutdown/completed
		oldTasksShutdown := true
		for _, task := range currentTasks {
			if oldTaskIDs[task.ID] {
				// Old task still exists - check if it's running
				if task.Status.State == swarm.TaskStateRunning {
					oldTasksShutdown = false
					break
				}
			}
		}

		// Check if new tasks exist and are running
		hasNewRunningTasks := false
		newTasksHealthy := true
		for _, task := range currentTasks {
			// Skip old tasks
			if oldTaskIDs[task.ID] {
				continue
			}

			// This is a new task
			if task.DesiredState == swarm.TaskStateRunning {
				if task.Status.State == swarm.TaskStateRunning {
					hasNewRunningTasks = true

					// Check container health if task has a container
					if task.Status.ContainerStatus.ContainerID != "" {
						containerID := task.Status.ContainerStatus.ContainerID
						inspect, err := d.cli.ContainerInspect(ctx, containerID)
						if err == nil {
							// If container has healthcheck, wait for it to be healthy
							if inspect.State.Health != nil {
								healthStatus := inspect.State.Health.Status
								// Allow "starting" as transitional state
								if healthStatus != "healthy" && healthStatus != "starting" {
									newTasksHealthy = false
								}
							}
						}
					}
				} else if task.Status.State != swarm.TaskStateComplete {
					// Task is not running yet and not complete - still starting
					hasNewRunningTasks = false
					break
				}
			}
		}

		// If old tasks are shutdown and new tasks are running and healthy, we're done
		if oldTasksShutdown && hasNewRunningTasks && newTasksHealthy {
			log.Printf("Service update completed: old tasks shutdown, new tasks running and healthy")
			return nil
		}

		time.Sleep(2 * time.Second)
	}
}
