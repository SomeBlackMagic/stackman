//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"stackman/internal/snapshot"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	dockerswarm "github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"

	"stackman/internal/compose"
	"stackman/internal/health"
	"stackman/internal/swarm"
)

// -----------------------------------
var (
	version  string = "dev"
	revision string = "000000000000000000000000000000"
)

//-----------------------------------

func main() {
	if len(os.Args) < 3 {
		log.Fatalf("Usage: %s <stack-name> <docker-compose.yml> [health-timeout-minutes] [max-failed-tasks]", os.Args[0])
	}

	log.Printf("Start Docker Stack Wait version=%s revision=%s", version, revision)

	stackName := os.Args[1]
	composeFile := os.Args[2]

	// Get health check timeout (default 1 minute)
	healthTimeout := 1 * time.Minute
	if len(os.Args) >= 4 {
		minutes, err := strconv.Atoi(os.Args[3])
		if err != nil {
			log.Printf("Invalid timeout value, using default: 1 minute")
		} else {
			healthTimeout = time.Duration(minutes) * time.Minute
			log.Printf("Health check timeout set to: %d minutes", minutes)
		}
	}

	// Check environment variable override
	if envTimeout := os.Getenv("HEALTH_TIMEOUT_MINUTES"); envTimeout != "" {
		minutes, err := strconv.Atoi(envTimeout)
		if err == nil {
			healthTimeout = time.Duration(minutes) * time.Minute
			log.Printf("Health check timeout from env: %d minutes", minutes)
		}
	}

	// Get max failed tasks count (default 3)
	maxFailedTaskCount := 3
	if len(os.Args) >= 5 {
		count, err := strconv.Atoi(os.Args[4])
		if err != nil {
			log.Printf("Invalid max-failed-tasks value, using default: 3")
		} else {
			maxFailedTaskCount = count
			log.Printf("Max failed tasks count set to: %d", count)
		}
	}

	// Check environment variable override
	if envMaxFailed := os.Getenv("MAX_FAILED_TASKS"); envMaxFailed != "" {
		count, err := strconv.Atoi(envMaxFailed)
		if err == nil {
			maxFailedTaskCount = count
			log.Printf("Max failed tasks count from env: %d", count)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout+5*time.Minute)
	defer cancel()

	// Setup signal handling for graceful shutdown and rollback
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Initialize Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("docker client init: %v", err)
	}
	defer cli.Close()

	// Parse compose file
	log.Printf("Parsing compose file: %s", composeFile)
	composeSpec, err := compose.ParseComposeFile(composeFile)
	if err != nil {
		log.Fatalf("failed to parse compose file: %v", err)
	}

	// Create deployer
	stackDeployer := swarm.NewStackDeployer(cli, stackName, maxFailedTaskCount)

	// Create snapshot before deployment for rollback
	snap := snapshot.CreateSnapshot(ctx, stackDeployer)

	// Track deployment state for signal handler
	deploymentComplete := make(chan bool, 1)

	// Handle signals in background
	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v", sig)

		select {
		case <-deploymentComplete:
			// Deployment already completed, just exit
			log.Println("Deployment already completed, exiting...")
			os.Exit(0)
		default:
			// Deployment in progress, rollback
			log.Println("Deployment interrupted, initiating rollback...")
			snapshot.Rollback(context.Background(), stackDeployer, snap)
			os.Exit(130) // Standard exit code for SIGINT
		}
	}()

	//// Start log streaming BEFORE deployment to catch all container events
	//logStreamer := monitor.NewLogStreamer(cli, stackName)
	//go logStreamer.StreamLogs(ctx)

	//// Start event streaming BEFORE deployment
	//eventStreamer := monitor.NewEventStreamer(cli, stackName)
	//go eventStreamer.StreamEvents(ctx)
	//
	//// Start health log streaming BEFORE deployment
	//healthLogStreamer := monitor.NewHealthLogStreamer(cli, stackName)
	//go healthLogStreamer.StreamHealthLogs(ctx)
	//
	// [STEP 1] Deploy stack (non-blocking, returns service IDs immediately)
	log.Printf("Deploying stack: %s", stackName)
	deployResult, err := stackDeployer.Deploy(ctx, composeSpec)
	if err != nil {
		log.Fatalf("failed to deploy stack: %v", err)
	}

	// [STEP 2] Start TaskWatcher and TaskMonitors for EACH updated service
	if len(deployResult.UpdatedServices) > 0 {
		log.Printf("Services updated/created: %d", len(deployResult.UpdatedServices))
		for _, svc := range deployResult.UpdatedServices {
			log.Printf("  - %s (version: %d)", svc.ServiceName, svc.Version.Index)
		}

		// Start dedicated watcher and monitor for each updated service
		log.Println("[TaskMonitor] Starting watchers and monitors for updated services...")

		var wg sync.WaitGroup
		updateErrors := make(chan error, len(deployResult.UpdatedServices))

		for _, svc := range deployResult.UpdatedServices {
			wg.Add(1)

			// Start service update monitor for each service
			go func(s swarm.ServiceUpdateResult) {
				defer wg.Done()

				// Monitor service update status
				updateMonitor := health.NewServiceUpdateMonitor(cli, s.ServiceID, s.ServiceName)
				if err := updateMonitor.WaitForUpdateComplete(ctx); err != nil {
					log.Printf("[ServiceUpdateMonitor] ❌ Service %s update failed: %v", s.ServiceName, err)
					updateErrors <- fmt.Errorf("service %s update failed: %w", s.ServiceName, err)
					return
				}

				log.Printf("[ServiceUpdateMonitor] ✅ Service %s update completed successfully", s.ServiceName)
			}(svc)

			// Create dedicated watcher filtered for this service and version
			serviceWatcher := health.NewServiceWatcher(cli, stackName, svc.ServiceID, svc.Version.Index)
			serviceEventsChan := serviceWatcher.Subscribe()

			// Start watcher in background
			go func(w *health.Watcher, svcName string) {
				if err := w.Start(ctx); err != nil && err != context.Canceled {
					log.Printf("[TaskWatcher] Error for service %s: %v", svcName, err)
				}
			}(serviceWatcher, svc.ServiceName)

			// Start monitor for this service
			go monitorServiceTasks(ctx, cli, svc, serviceEventsChan)

			log.Printf("[TaskMonitor] Started watcher for service %s version %d+", svc.ServiceName, svc.Version.Index)
		}

		// Wait for all service updates to complete
		go func() {
			wg.Wait()
			close(updateErrors)
		}()

		// Check if any updates failed
		for err := range updateErrors {
			if err != nil {
				log.Printf("ERROR: %v", err)
				snapshot.Rollback(ctx, stackDeployer, snap)
				os.Exit(1)
			}
		}

		log.Println("[ServiceUpdateMonitor] All service updates completed successfully")

		// Now wait for all tasks to become healthy
		log.Println("[TaskMonitor] Waiting for all tasks to become healthy...")

		// Create health check context with timeout
		healthCtx, healthCancel := context.WithTimeout(ctx, healthTimeout)
		defer healthCancel()

		// Wait for all tasks to report healthy status
		if err := waitForAllTasksHealthy(healthCtx, cli, stackName, deployResult.UpdatedServices); err != nil {
			log.Printf("ERROR: %v", err)
			snapshot.Rollback(ctx, stackDeployer, snap)
			os.Exit(1)
		}

		log.Println("[TaskMonitor] All tasks are healthy")
	} else {
		log.Printf("No services were changed during this deployment")
	}

	fmt.Println("All containers healthy.")

	// Mark deployment as successful
	deploymentComplete <- true

	cancel()
}

// waitForAllTasksHealthy waits for all tasks of updated services to become healthy
func waitForAllTasksHealthy(ctx context.Context, cli *client.Client, stackName string, updatedServices []swarm.ServiceUpdateResult) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			// Show which services didn't become healthy before timeout
			unhealthyServices := []string{}
			for _, svc := range updatedServices {
				filter := filters.NewArgs()
				filter.Add("service", svc.ServiceID)
				filter.Add("desired-state", "running")

				tasks, err := cli.TaskList(context.Background(), types.TaskListOptions{
					Filters: filter,
				})
				if err == nil {
					hasHealthy := false
					for _, t := range tasks {
						if t.Version.Index >= svc.Version.Index && t.Status.State == dockerswarm.TaskStateRunning {
							if t.Status.ContainerStatus != nil && t.Status.ContainerStatus.ContainerID != "" {
								containerInfo, err := cli.ContainerInspect(context.Background(), t.Status.ContainerStatus.ContainerID)
								if err == nil {
									if containerInfo.State.Health == nil || containerInfo.State.Health.Status == "healthy" {
										hasHealthy = true
										break
									}
								}
							}
						}
					}
					if !hasHealthy {
						unhealthyServices = append(unhealthyServices, svc.ServiceName)
					}
				}
			}

			elapsed := time.Since(startTime).Round(time.Second)
			if len(unhealthyServices) > 0 {
				return fmt.Errorf("timeout after %v waiting for services to become healthy: %v", elapsed, unhealthyServices)
			}
			return fmt.Errorf("timeout after %v waiting for tasks to become healthy", elapsed)

		case <-ticker.C:
			allHealthy := true
			unhealthyTasks := []string{}
			serviceHealthyCount := make(map[string]int)

			for _, svc := range updatedServices {
				// Get ALL tasks for this service (including failed/shutdown)
				filter := filters.NewArgs()
				filter.Add("service", svc.ServiceID)

				tasks, err := cli.TaskList(ctx, types.TaskListOptions{
					Filters: filter,
				})
				if err != nil {
					log.Printf("[HealthCheck] Failed to list tasks for service %s: %v", svc.ServiceName, err)
					allHealthy = false
					continue
				}

				healthyTaskCount := 0
				hasRunningTask := false

				for _, t := range tasks {
					// Skip tasks from old versions
					if t.Version.Index < svc.Version.Index {
						continue
					}

					// Log failed/shutdown tasks but don't fail immediately (Docker Swarm may restart)
					if t.Status.State == dockerswarm.TaskStateFailed ||
						t.Status.State == dockerswarm.TaskStateShutdown ||
						t.Status.State == dockerswarm.TaskStateRejected {
						log.Printf("[HealthCheck] ⚠️  Task %s (%s) is %s: %s (waiting for restart)",
							t.ID[:12], svc.ServiceName, t.Status.State, t.Status.Message)
						continue
					}

					// Only check tasks with desired-state=running
					if t.DesiredState != dockerswarm.TaskStateRunning {
						continue
					}

					hasRunningTask = true

					// Check if task is running
					if t.Status.State != dockerswarm.TaskStateRunning {
						allHealthy = false
						unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s/%s (state: %s)", svc.ServiceName, t.ID[:12], t.Status.State))
						log.Printf("[HealthCheck] ⏳ Task %s (%s) is %s", t.ID[:12], svc.ServiceName, t.Status.State)
						continue
					}

					// Check container health if healthcheck is defined
					if t.Status.ContainerStatus != nil && t.Status.ContainerStatus.ContainerID != "" {
						containerInfo, err := cli.ContainerInspect(ctx, t.Status.ContainerStatus.ContainerID)
						if err != nil {
							log.Printf("[HealthCheck] Failed to inspect container %s for task %s (%s): %v",
								t.Status.ContainerStatus.ContainerID[:12], t.ID[:12], svc.ServiceName, err)
							allHealthy = false
							unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s/%s (inspect failed)", svc.ServiceName, t.ID[:12]))
							continue
						}

						// If container has health check, wait for healthy status
						if containerInfo.State.Health != nil {
							if containerInfo.State.Health.Status != "healthy" {
								allHealthy = false
								unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s/%s (health: %s)", svc.ServiceName, t.ID[:12], containerInfo.State.Health.Status))
								log.Printf("[HealthCheck] ⏳ Task %s (%s) is %s", t.ID[:12], svc.ServiceName, containerInfo.State.Health.Status)
							} else {
								log.Printf("[HealthCheck] ✅ Task %s (%s) is healthy", t.ID[:12], svc.ServiceName)
								healthyTaskCount++
							}
						} else {
							// No healthcheck defined, just check if running
							log.Printf("[HealthCheck] ✅ Task %s (%s) is running (no healthcheck)", t.ID[:12], svc.ServiceName)
							healthyTaskCount++
						}
					} else {
						// No container status yet
						allHealthy = false
						unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s/%s (no container)", svc.ServiceName, t.ID[:12]))
						log.Printf("[HealthCheck] ⏳ Task %s (%s) has no container yet", t.ID[:12], svc.ServiceName)
					}
				}

				// Track if service has running tasks
				if !hasRunningTask {
					log.Printf("[HealthCheck] ⏳ Service %s has no running tasks yet (may be restarting)", svc.ServiceName)
					allHealthy = false
				}

				serviceHealthyCount[svc.ServiceName] = healthyTaskCount
			}

			// Check that all services have at least one healthy task
			for _, svc := range updatedServices {
				if serviceHealthyCount[svc.ServiceName] == 0 {
					allHealthy = false
				}
			}

			if allHealthy {
				return nil
			}

			if len(unhealthyTasks) > 0 {
				log.Printf("[HealthCheck] Waiting for %d task(s): %v", len(unhealthyTasks), unhealthyTasks)
			}
		}
	}
}

// monitorServiceTasks creates TaskMonitor for each task in the service and monitors them
func monitorServiceTasks(ctx context.Context, cli *client.Client, svc swarm.ServiceUpdateResult, eventChan <-chan health.Event) {
	log.Printf("[ServiceMonitor] Started monitoring service: %s (version: %d)", svc.ServiceName, svc.Version.Index)

	// Track active task monitors
	taskMonitors := make(map[string]*health.Monitor)
	var mu sync.Mutex

	// Create cleanup goroutine
	defer func() {
		mu.Lock()
		for taskID, monitor := range taskMonitors {
			log.Printf("[ServiceMonitor] Stopping monitor for task %s", taskID[:12])
			monitor.Stop()
		}
		mu.Unlock()
		log.Printf("[ServiceMonitor] Stopped monitoring service: %s", svc.ServiceName)
	}()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[ServiceMonitor] Context cancelled for service %s", svc.ServiceName)
			return

		case event, ok := <-eventChan:
			if !ok {
				log.Printf("[ServiceMonitor] Event channel closed for service %s", svc.ServiceName)
				return
			}

			taskID := event.TaskID
			mu.Lock()
			monitor, exists := taskMonitors[taskID]

			// Create new monitor for new tasks
			if !exists && event.Type == health.EventTypeCreated {
				log.Printf("[ServiceMonitor] New task detected: %s for service %s",
					taskID[:12], svc.ServiceName)

				monitor = health.NewMonitor(cli, taskID, svc.ServiceID, svc.ServiceName)
				taskMonitors[taskID] = monitor

				// Start monitor in background
				go func(m *health.Monitor) {
					if err := m.Start(ctx); err != nil && err != context.Canceled {
						log.Printf("[ServiceMonitor] Monitor error for task %s: %v", taskID[:12], err)
					}

					// Remove from registry when done
					mu.Lock()
					delete(taskMonitors, taskID)
					mu.Unlock()
					log.Printf("[ServiceMonitor] Task %s monitor finished", taskID[:12])
				}(monitor)
			}

			// Send event to existing monitor
			if monitor != nil {
				monitor.SendEvent(event)
			}

			mu.Unlock()

			// Log important events at service level
			switch event.Type {
			case health.EventTypeCreated:
				log.Printf("[ServiceMonitor] 🆕 Service %s: Task %s created",
					svc.ServiceName, taskID[:12])
			case health.EventTypeFailed:
				log.Printf("[ServiceMonitor] ❌ Service %s: Task %s failed - %s",
					svc.ServiceName, taskID[:12], event.Message)
			case health.EventTypeHealthy:
				log.Printf("[ServiceMonitor] 💚 Service %s: Task %s is healthy",
					svc.ServiceName, taskID[:12])
			}
		}
	}
}

// logTaskEvents logs task lifecycle events from TaskWatcher
// This function demonstrates the new task event system in action
func logTaskEvents(ctx context.Context, eventChan <-chan health.Event) {
	log.Println("[TaskWatcher] Started logging task events")

	for {
		select {
		case <-ctx.Done():
			log.Println("[TaskWatcher] Stopped logging task events")
			return

		case event, ok := <-eventChan:
			if !ok {
				log.Println("[TaskWatcher] Event channel closed")
				return
			}

			// Log event with color-coded prefix based on event type
			prefix := getEventPrefix(event.Type)
			shortTaskID := shortenID(event.TaskID)
			shortContainerID := shortenID(event.ContainerID)

			log.Printf("[TaskWatcher] %s Task: %s | Service: %s | Container: %s | %s",
				prefix,
				shortTaskID,
				event.ServiceName,
				shortContainerID,
				event.Message,
			)

			// Log additional details for failure events
			if event.IsFailure() && event.Error != nil {
				log.Printf("[TaskWatcher]   └─ Error: %v", event.Error)
			}

			// Log state transitions
			if event.State != "" && event.DesiredState != "" {
				log.Printf("[TaskWatcher]   └─ State: %s → %s", event.State, event.DesiredState)
			}
		}
	}
}

// getEventPrefix returns a visual prefix for different event types
func getEventPrefix(eventType health.EventType) string {
	switch eventType {
	case health.EventTypeCreated:
		return "🆕"
	case health.EventTypeStarted:
		return "▶️ "
	case health.EventTypeRunning:
		return "✅"
	case health.EventTypeHealthy:
		return "💚"
	case health.EventTypeUnhealthy:
		return "💔"
	case health.EventTypeFailed:
		return "❌"
	case health.EventTypeCompleted:
		return "🏁"
	case health.EventTypeShutdown:
		return "🛑"
	default:
		return "ℹ️ "
	}
}

// shortenID shortens Docker IDs to first 12 characters for readability
func shortenID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
