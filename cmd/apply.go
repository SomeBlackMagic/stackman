package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerswarm "github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"

	"github.com/SomeBlackMagic/stackman/internal/compose"
	"github.com/SomeBlackMagic/stackman/internal/deployment"
	"github.com/SomeBlackMagic/stackman/internal/health"
	"github.com/SomeBlackMagic/stackman/internal/snapshot"
	"github.com/SomeBlackMagic/stackman/internal/swarm"
)

// errSignalInterrupted is returned when deployment is stopped by OS signal.
var errSignalInterrupted = errors.New("deployment interrupted by signal")

// ExecuteApply runs the apply command
func ExecuteApply(args []string) {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)

	// Required flags
	stackName := fs.String("n", "", "Stack name (required)")
	composeFile := fs.String("f", "", "Compose file path (required)")

	// Optional flags
	valuesFile := fs.String("values", "", "Values file for templating")
	setValues := fs.String("set", "", "Set values (comma-separated key=value pairs)")
	timeout := fs.Duration("timeout", 15*time.Minute, "Deployment timeout")
	rollbackTimeout := fs.Duration("rollback-timeout", 10*time.Minute, "Rollback timeout")
	noWait := fs.Bool("no-wait", false, "Don't wait for deployment to complete")
	prune := fs.Bool("prune", false, "Remove orphaned resources")
	allowLatest := fs.Bool("allow-latest", false, "Allow 'latest' tag in images")
	parallel := fs.Int("parallel", 1, "Number of parallel service updates")
	showLogs := fs.Bool("logs", true, "Show container logs during deployment")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: stackman apply -n <stack> -f <compose-file> [flags]

Deploy or update a Docker Swarm stack.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Validate required flags
	if *stackName == "" {
		fmt.Fprintf(os.Stderr, "Error: -n (stack name) is required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	if *composeFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -f (compose file) is required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	// Run apply logic
	if err := runApply(*stackName, *composeFile, &ApplyOptions{
		ValuesFile:      *valuesFile,
		SetValues:       *setValues,
		Timeout:         *timeout,
		RollbackTimeout: *rollbackTimeout,
		NoWait:          *noWait,
		Prune:           *prune,
		AllowLatest:     *allowLatest,
		Parallel:        *parallel,
		ShowLogs:        *showLogs,
	}); err != nil {
		if errors.Is(err, errSignalInterrupted) {
			os.Exit(130)
		}
		log.Fatalf("Apply failed: %v", err)
	}
}

// ApplyOptions contains options for the apply command
type ApplyOptions struct {
	ValuesFile      string
	SetValues       string
	Timeout         time.Duration
	RollbackTimeout time.Duration
	NoWait          bool
	Prune           bool
	AllowLatest     bool
	Parallel        int
	ShowLogs        bool
}

// runApply performs the actual deployment
func runApply(stackName, composeFile string, opts *ApplyOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout+5*time.Minute)
	defer cancel()

	// watcherWg tracks service watcher and task monitor goroutines.
	// The deferred call cancels the context and waits for all goroutines to exit
	// regardless of whether runApply returns normally or with an error.
	var watcherWg sync.WaitGroup
	defer func() {
		cancel()
		watcherWg.Wait()
	}()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Initialize Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("docker client init: %w", err)
	}
	defer cli.Close()

	// Parse compose file
	log.Printf("Parsing compose file: %s", composeFile)
	composeSpec, err := compose.ParseComposeFile(composeFile)
	if err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	// TODO: Apply templating if valuesFile or setValues provided

	// Generate deployment ID
	deployID := deployment.GenerateDeployID()
	log.Printf("[Deploy] Generated deployment ID: %s", deployID)

	// Create deployer
	stackDeployer := swarm.NewStackDeployer(cli, stackName, 3)

	// Create snapshot before deployment
	snap, err := snapshot.CreateSnapshot(ctx, stackDeployer)
	if err != nil {
		return fmt.Errorf("deployment blocked: %w", err)
	}

	// deploymentComplete is closed when deployment finishes (success or no-wait).
	// interrupted is closed when a signal triggers rollback.
	deploymentComplete := make(chan struct{})
	interrupted := make(chan struct{})

	// Handle signals: roll back and propagate cancellation instead of os.Exit,
	// so deferred cleanups and goroutine waits run normally.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Signal] PANIC in signal handler: %v", r)
				cancel()
			}
		}()
		select {
		case sig := <-sigChan:
			log.Printf("Received signal: %v", sig)
			select {
			case <-deploymentComplete:
				log.Println("Deployment already completed")
			default:
				log.Println("Deployment interrupted, initiating rollback...")
				rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer rollbackCancel()
				snapshot.Rollback(rollbackCtx, stackDeployer, snap)
				close(interrupted)
			}
			cancel() // stop all goroutines
		case <-ctx.Done():
			// Context expired naturally; nothing to do
		}
	}()

	// Deploy stack
	log.Printf("Deploying stack: %s (DeployID: %s)", stackName, deployID)
	deployResult, err := stackDeployer.Deploy(ctx, composeSpec, deployID)
	if err != nil {
		return fmt.Errorf("failed to deploy stack: %w", err)
	}

	fmt.Println("Stack deployed successfully.")

	// If --no-wait, exit now
	if opts.NoWait {
		close(deploymentComplete)
		return nil
	}

	// Wait for services to become healthy
	if len(deployResult.UpdatedServices) > 0 {
		log.Printf("Services updated/created: %d", len(deployResult.UpdatedServices))
		for _, svc := range deployResult.UpdatedServices {
			log.Printf("  - %s (version: %d)", svc.ServiceName, svc.Version.Index)
		}

		// Start event-driven monitoring with watchers
		log.Println("[TaskMonitor] Starting watchers and monitors for updated services...")
		if opts.ShowLogs {
			log.Println("[TaskMonitor] Container logs will be streamed below...")
		}

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

			// Create dedicated watcher filtered for this service, version and deployID
			serviceWatcher := health.NewServiceWatcher(cli, stackName, svc.ServiceID, svc.Version.Index, deployResult.DeployID)
			serviceEventsChan := serviceWatcher.Subscribe()

			// Start watcher in background — tracked by watcherWg
			watcherWg.Add(1)
			go func(w *health.Watcher, svcName string) {
				defer watcherWg.Done()
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[TaskWatcher] PANIC for service %s: %v", svcName, r)
					}
				}()
				if err := w.Start(ctx); err != nil && err != context.Canceled {
					log.Printf("[TaskWatcher] Error for service %s: %v", svcName, err)
				}
			}(serviceWatcher, svc.ServiceName)

			// Start monitor for this service — tracked by watcherWg
			watcherWg.Add(1)
			go func(s swarm.ServiceUpdateResult, evChan <-chan health.Event) {
				defer watcherWg.Done()
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[ServiceMonitor] PANIC for service %s: %v", s.ServiceName, r)
					}
				}()
				monitorServiceTasks(ctx, cli, s, evChan, opts.ShowLogs, deployResult.DeployID)
			}(svc, serviceEventsChan)

			log.Printf("[TaskMonitor] Started watcher for service %s version %d+ (deployID: %s)", svc.ServiceName, svc.Version.Index, deployResult.DeployID)
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
				return err
			}
		}

		log.Println("[ServiceUpdateMonitor] All service updates completed successfully")

		// Now wait for all tasks to become healthy
		log.Println("[TaskMonitor] Waiting for all tasks to become healthy...")

		// Create health check context with timeout
		healthCtx, healthCancel := context.WithTimeout(ctx, opts.Timeout)
		defer healthCancel()

		// Wait for all tasks to report healthy status
		if err := waitForAllTasksHealthy(healthCtx, cli, stackName, deployResult.UpdatedServices, deployResult.DeployID); err != nil {
			log.Printf("ERROR: %v", err)
			snapshot.Rollback(ctx, stackDeployer, snap)
			return err
		}

		log.Println("[TaskMonitor] All tasks are healthy")
	} else {
		log.Println("No services were changed during this deployment")
	}

	// Signal that deployment finished (used by the signal handler goroutine)
	close(deploymentComplete)

	// If a signal triggered rollback during deployment, propagate as error
	select {
	case <-interrupted:
		return errSignalInterrupted
	default:
		return nil
	}
}

// monitorServiceTasks monitors task lifecycle events for a service and logs them
func monitorServiceTasks(ctx context.Context, cli *client.Client, svc swarm.ServiceUpdateResult, eventChan <-chan health.Event, showLogs bool, deployID string) {
	log.Printf("[ServiceMonitor] Started monitoring service: %s (version: %d, deployID: %s)", svc.ServiceName, svc.Version.Index, deployID)

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

				monitor = health.NewMonitorWithLogs(cli, taskID, svc.ServiceID, svc.ServiceName, showLogs)
				taskMonitors[taskID] = monitor

				// Start monitor in background
				go func(m *health.Monitor) {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[ServiceMonitor] PANIC for task %s: %v", taskID[:12], r)
						}
					}()
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
			case health.EventTypeUnhealthy:
				log.Printf("[ServiceMonitor] 💔 Service %s: Task %s is unhealthy - %s",
					svc.ServiceName, taskID[:12], event.Message)
			case health.EventTypeRunning:
				log.Printf("[ServiceMonitor] ✅ Service %s: Task %s is running",
					svc.ServiceName, taskID[:12])
			}
		}
	}
}

// waitForAllTasksHealthy waits for all tasks of updated services to become healthy
// Optimized to use batch API calls instead of per-service calls
func waitForAllTasksHealthy(ctx context.Context, cli *client.Client, stackName string, updatedServices []swarm.ServiceUpdateResult, deployID string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			elapsed := time.Since(startTime).Round(time.Second)
			return fmt.Errorf("timeout after %v waiting for services to become healthy", elapsed)

		case <-ticker.C:
			allHealthy := true
			unhealthyTasks := []string{}
			serviceHealthyCount := make(map[string]int)

			// OPTIMIZATION 1: Batch fetch all services in stack with one API call
			serviceFilter := filters.NewArgs()
			serviceFilter.Add("label", "com.docker.stack.namespace="+stackName)
			allServices, err := cli.ServiceList(ctx, types.ServiceListOptions{
				Filters: serviceFilter,
			})
			if err != nil {
				log.Printf("[HealthCheck] Failed to list services: %v", err)
				allHealthy = false
				continue
			}

			// Create service name -> service map for quick lookup
			serviceMap := make(map[string]dockerswarm.Service)
			for _, svc := range allServices {
				serviceMap[svc.Spec.Name] = svc
			}

			// OPTIMIZATION 2: Batch fetch all tasks in stack with one API call
			taskFilter := filters.NewArgs()
			taskFilter.Add("label", "com.docker.stack.namespace="+stackName)
			allStackTasks, err := cli.TaskList(ctx, types.TaskListOptions{
				Filters: taskFilter,
			})
			if err != nil {
				log.Printf("[HealthCheck] Failed to list tasks: %v", err)
				allHealthy = false
				continue
			}

			// Group tasks by service name and filter by deployID
			tasksByService := make(map[string][]dockerswarm.Task)
			for _, task := range allStackTasks {
				// Filter by deployID
				if task.Spec.ContainerSpec != nil && task.Spec.ContainerSpec.Labels != nil {
					if taskDeployID, ok := task.Spec.ContainerSpec.Labels["com.stackman.deploy.id"]; ok && taskDeployID == deployID {
						serviceName := task.Spec.ContainerSpec.Labels["com.docker.swarm.service.name"]
						tasksByService[serviceName] = append(tasksByService[serviceName], task)
					}
				}
			}

			// OPTIMIZATION 3: Collect containers that need inspection
			type containerTask struct {
				containerID string
				task        dockerswarm.Task
				serviceName string
			}
			containersToInspect := []containerTask{}

			// Process each updated service
			for _, svc := range updatedServices {
				_, exists := serviceMap[svc.ServiceName]
				if !exists {
					log.Printf("[HealthCheck] Service %s not found", svc.ServiceName)
					allHealthy = false
					continue
				}

				tasks := tasksByService[svc.ServiceName]
				log.Printf("[HealthCheck] Service %s: found %d tasks with deployID %s",
					svc.ServiceName, len(tasks), deployID)

				hasRunningTask := false

				for _, t := range tasks {
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

					// Mark container for inspection if it exists
					if t.Status.ContainerStatus != nil && t.Status.ContainerStatus.ContainerID != "" {
						containersToInspect = append(containersToInspect, containerTask{
							containerID: t.Status.ContainerStatus.ContainerID,
							task:        t,
							serviceName: svc.ServiceName,
						})
					} else {
						allHealthy = false
						unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s/%s (no container)", svc.ServiceName, t.ID[:12]))
						log.Printf("[HealthCheck] ⏳ Task %s (%s) has no container yet", t.ID[:12], svc.ServiceName)
					}
				}

				if !hasRunningTask {
					log.Printf("[HealthCheck] ⏳ Service %s has no running tasks yet (may be restarting)", svc.ServiceName)
					allHealthy = false
				}
			}

			// OPTIMIZATION 4: Parallel container inspections with goroutines.
			// A semaphore caps concurrency so we don't fan-out unboundedly.
			type inspectResult struct {
				ct   containerTask
				info types.ContainerJSON
				err  error
			}

			const maxParallelInspections = 10
			sem := make(chan struct{}, maxParallelInspections)
			resultChan := make(chan inspectResult, len(containersToInspect))
			var wg sync.WaitGroup

			for _, ct := range containersToInspect {
				wg.Add(1)
				go func(c containerTask) {
					defer wg.Done()
					sem <- struct{}{}         // acquire semaphore slot
					defer func() { <-sem }() // release on exit
					info, err := cli.ContainerInspect(ctx, c.containerID)
					resultChan <- inspectResult{ct: c, info: info, err: err}
				}(ct)
			}

			go func() {
				wg.Wait()
				close(resultChan)
			}()

			// Process inspection results
			for result := range resultChan {
				ct := result.ct
				taskID := ct.task.ID
				serviceName := ct.serviceName

				if result.err != nil {
					log.Printf("[HealthCheck] Failed to inspect container %s for task %s (%s): %v",
						ct.containerID[:12], taskID[:12], serviceName, result.err)
					allHealthy = false
					unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s/%s (inspect failed)", serviceName, taskID[:12]))
					continue
				}

				// Check health status
				if result.info.State.Health != nil {
					if result.info.State.Health.Status != container.Healthy {
						allHealthy = false
						unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s/%s (health: %s)",
							serviceName, taskID[:12], result.info.State.Health.Status))
						log.Printf("[HealthCheck] ⏳ Task %s (%s) is %s",
							taskID[:12], serviceName, result.info.State.Health.Status)
					} else {
						log.Printf("[HealthCheck] ✅ Task %s (%s) is healthy", taskID[:12], serviceName)
						serviceHealthyCount[serviceName]++
					}
				} else {
					// No healthcheck defined, just check if running
					log.Printf("[HealthCheck] ✅ Task %s (%s) is running (no healthcheck)", taskID[:12], serviceName)
					serviceHealthyCount[serviceName]++
				}
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
				log.Printf("[HealthCheck] Waiting for: %v", unhealthyTasks)
			}
		}
	}
}
