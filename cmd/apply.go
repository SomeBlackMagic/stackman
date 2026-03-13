package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerswarm "github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"golang.org/x/sync/errgroup"

	flag "github.com/spf13/pflag"

	"github.com/SomeBlackMagic/stackman/internal/compose"
	"github.com/SomeBlackMagic/stackman/internal/deployment"
	"github.com/SomeBlackMagic/stackman/internal/health"
	"github.com/SomeBlackMagic/stackman/internal/snapshot"
	"github.com/SomeBlackMagic/stackman/internal/swarm"
)

// ExecuteApply runs the apply command
func ExecuteApply(args []string) {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)

	// Required flags
	stackName := fs.StringP("name", "n", "", "Stack name (required)")
	composeFile := fs.StringP("file", "f", "", "Compose file path (required)")

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
		fmt.Fprintf(os.Stderr, "Error: -n/--name (stack name) is required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	if *composeFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -f/--file (compose file) is required\n\n")
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

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigChan)

	// Initialize Docker client
	cli, err := newDockerClient()
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
	stackDeployer := swarm.NewStackDeployerWithLogger(cli, stackName, 3, log.Default())

	// Create snapshot before deployment
	snap, err := snapshot.CreateSnapshot(ctx, stackDeployer)
	if err != nil {
		return fmt.Errorf("deployment blocked: %w", err)
	}

	deploymentComplete := make(chan struct{})
	defer close(deploymentComplete)

	signalErrCh := make(chan error, 1)
	var signalWG sync.WaitGroup
	signalWG.Add(1)
	go func() {
		defer signalWG.Done()
		runSignalHandler(ctx, sigChan, deploymentComplete, snap, stackDeployer, opts.RollbackTimeout, cancel, signalErrCh)
	}()
	defer signalWG.Wait()

	// Deploy stack
	log.Printf("Deploying stack: %s (DeployID: %s)", stackName, deployID)
	deployResult, err := stackDeployer.Deploy(ctx, composeSpec, deployID)
	if err != nil {
		if signalErr := tryGetSignalError(signalErrCh); signalErr != nil {
			return signalErr
		}
		return fmt.Errorf("failed to deploy stack: %w", err)
	}

	fmt.Println("Stack deployed successfully.")

	// If --no-wait, exit now
	if opts.NoWait {
		if signalErr := tryGetSignalError(signalErrCh); signalErr != nil {
			return signalErr
		}
		return nil
	}

	if len(deployResult.UpdatedServices) == 0 {
		log.Println("No services were changed during this deployment")
		if signalErr := tryGetSignalError(signalErrCh); signalErr != nil {
			return signalErr
		}
		return nil
	}

	log.Printf("Services updated/created: %d", len(deployResult.UpdatedServices))
	for _, svc := range deployResult.UpdatedServices {
		log.Printf("  - %s (version: %d)", svc.ServiceName, svc.Version.Index)
	}

	// Start event-driven monitoring with watchers.
	log.Println("[TaskMonitor] Starting watchers and monitors for updated services...")
	if opts.ShowLogs {
		log.Println("[TaskMonitor] Container logs will be streamed below...")
	}

	updateCtx, updateCancel := context.WithCancel(ctx)
	defer updateCancel()

	watchersGroup, watchersCtx := errgroup.WithContext(updateCtx)
	for _, svc := range deployResult.UpdatedServices {
		svc := svc
		serviceWatcher := health.NewServiceWatcherWithLogger(cli, stackName, svc.ServiceID, svc.Version.Index, deployResult.DeployID, log.Default())
		serviceEventsChan := serviceWatcher.Subscribe()
		unsubscribe := func() { serviceWatcher.Unsubscribe(serviceEventsChan) }

		watchersGroup.Go(func() error {
			return runServiceWatcher(watchersCtx, serviceWatcher, svc.ServiceName)
		})

		watchersGroup.Go(func() error {
			return monitorServiceTasks(watchersCtx, cli, svc, serviceEventsChan, unsubscribe, opts.ShowLogs, deployResult.DeployID)
		})

		log.Printf("[TaskMonitor] Started watcher for service %s version %d+ (deployID: %s)", svc.ServiceName, svc.Version.Index, deployResult.DeployID)
	}

	updateGroup, updateGroupCtx := errgroup.WithContext(updateCtx)
	for _, svc := range deployResult.UpdatedServices {
		svc := svc
		updateGroup.Go(func() error {
			return runServiceUpdateMonitor(updateGroupCtx, cli, svc)
		})
	}

	if err := updateGroup.Wait(); err != nil {
		updateCancel()
		_ = watchersGroup.Wait()
		if signalErr := tryGetSignalError(signalErrCh); signalErr != nil {
			return signalErr
		}
		log.Printf("ERROR: %v", err)
		snapshot.Rollback(ctx, stackDeployer, snap)
		return err
	}

	log.Println("[ServiceUpdateMonitor] All service updates completed successfully")
	log.Println("[TaskMonitor] Waiting for all tasks to become healthy...")

	healthCtx, healthCancel := context.WithTimeout(updateCtx, opts.Timeout)
	defer healthCancel()

	inspectParallelism := opts.Parallel
	if inspectParallelism < 1 {
		inspectParallelism = 1
	}

	if err := waitForAllTasksHealthy(healthCtx, cli, stackName, deployResult.UpdatedServices, deployResult.DeployID, inspectParallelism); err != nil {
		updateCancel()
		_ = watchersGroup.Wait()
		if signalErr := tryGetSignalError(signalErrCh); signalErr != nil {
			return signalErr
		}
		log.Printf("ERROR: %v", err)
		snapshot.Rollback(ctx, stackDeployer, snap)
		return err
	}

	updateCancel()
	if err := watchersGroup.Wait(); err != nil && err != context.Canceled {
		if signalErr := tryGetSignalError(signalErrCh); signalErr != nil {
			return signalErr
		}
		return err
	}

	log.Println("[TaskMonitor] All tasks are healthy")

	if signalErr := tryGetSignalError(signalErrCh); signalErr != nil {
		return signalErr
	}

	return nil
}

// monitorServiceTasks monitors task lifecycle events for a service and logs them.
// unsubscribe must be called to release the event channel subscription on exit.
func monitorServiceTasks(ctx context.Context, cli *client.Client, svc swarm.ServiceUpdateResult, eventChan <-chan health.Event, unsubscribe func(), showLogs bool, deployID string) error {
	defer recoverPanic("monitorServiceTasks")
	log.Printf("[ServiceMonitor] Started monitoring service: %s (version: %d, deployID: %s)", svc.ServiceName, svc.Version.Index, deployID)

	// Track active task monitors
	taskMonitors := make(map[string]*health.Monitor)
	var monitorsMu sync.Mutex
	var workersWG sync.WaitGroup

	defer func() {
		unsubscribe()
		monitorsMu.Lock()
		for taskID, monitor := range taskMonitors {
			log.Printf("[ServiceMonitor] Stopping monitor for task %s", shortTaskID(taskID))
			monitor.Stop()
		}
		monitorsMu.Unlock()
		workersWG.Wait()
		log.Printf("[ServiceMonitor] Stopped monitoring service: %s", svc.ServiceName)
	}()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[ServiceMonitor] Context cancelled for service %s", svc.ServiceName)
			return nil

		case event, ok := <-eventChan:
			if !ok {
				log.Printf("[ServiceMonitor] Event channel closed for service %s", svc.ServiceName)
				return nil
			}

			taskID := event.TaskID
			monitorsMu.Lock()
			monitor, exists := taskMonitors[taskID]

			// Create new monitor for new tasks
			if !exists && event.Type == health.EventTypeCreated {
				log.Printf("[ServiceMonitor] New task detected: %s for service %s", shortTaskID(taskID), svc.ServiceName)

				monitor = health.NewMonitorWithLogsAndLogger(cli, taskID, svc.ServiceID, svc.ServiceName, showLogs, log.Default())
				taskMonitors[taskID] = monitor

				workersWG.Add(1)
				go func(taskID string, monitor *health.Monitor) {
					defer workersWG.Done()
					runTaskMonitorWorker(ctx, monitor, taskID, taskMonitors, &monitorsMu)
				}(taskID, monitor)
			}

			// Send event to existing monitor
			if monitor != nil {
				monitor.SendEvent(event)
			}
			monitorsMu.Unlock()

			// Log important events at service level
			switch event.Type {
			case health.EventTypeCreated:
				log.Printf("[ServiceMonitor] 🆕 Service %s: Task %s created", svc.ServiceName, shortTaskID(taskID))
			case health.EventTypeFailed:
				log.Printf("[ServiceMonitor] ❌ Service %s: Task %s failed - %s", svc.ServiceName, shortTaskID(taskID), event.Message)
			case health.EventTypeHealthy:
				log.Printf("[ServiceMonitor] 💚 Service %s: Task %s is healthy", svc.ServiceName, shortTaskID(taskID))
			case health.EventTypeUnhealthy:
				log.Printf("[ServiceMonitor] 💔 Service %s: Task %s is unhealthy - %s", svc.ServiceName, shortTaskID(taskID), event.Message)
			case health.EventTypeRunning:
				log.Printf("[ServiceMonitor] ✅ Service %s: Task %s is running", svc.ServiceName, shortTaskID(taskID))
			}
		}
	}
}

// waitForAllTasksHealthy waits for all tasks of updated services to become healthy.
// Optimized to use batch API calls instead of per-service calls.
func waitForAllTasksHealthy(ctx context.Context, cli *client.Client, stackName string, updatedServices []swarm.ServiceUpdateResult, deployID string, inspectParallelism int) error {
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

			// Batch fetch all services in stack with one API call.
			serviceFilter := filters.NewArgs()
			serviceFilter.Add("label", "com.docker.stack.namespace="+stackName)
			allServices, err := cli.ServiceList(ctx, types.ServiceListOptions{Filters: serviceFilter})
			if err != nil {
				log.Printf("[HealthCheck] Failed to list services: %v", err)
				allHealthy = false
				continue
			}

			serviceMap := make(map[string]dockerswarm.Service)
			for _, svc := range allServices {
				serviceMap[svc.Spec.Name] = svc
			}

			// Batch fetch all tasks in stack with one API call.
			taskFilter := filters.NewArgs()
			taskFilter.Add("label", "com.docker.stack.namespace="+stackName)
			allStackTasks, err := cli.TaskList(ctx, types.TaskListOptions{Filters: taskFilter})
			if err != nil {
				log.Printf("[HealthCheck] Failed to list tasks: %v", err)
				allHealthy = false
				continue
			}

			// Group tasks by service name and filter by deployID.
			tasksByService := make(map[string][]dockerswarm.Task)
			for _, task := range allStackTasks {
				if task.Spec.ContainerSpec == nil || task.Spec.ContainerSpec.Labels == nil {
					continue
				}
				taskDeployID, ok := task.Spec.ContainerSpec.Labels["com.stackman.deploy.id"]
				if !ok || taskDeployID != deployID {
					continue
				}
				serviceName := task.Spec.ContainerSpec.Labels["com.docker.swarm.service.name"]
				tasksByService[serviceName] = append(tasksByService[serviceName], task)
			}

			containersToInspect := []containerTask{}
			for _, svc := range updatedServices {
				_, exists := serviceMap[svc.ServiceName]
				if !exists {
					log.Printf("[HealthCheck] Service %s not found", svc.ServiceName)
					allHealthy = false
					continue
				}

				tasks := tasksByService[svc.ServiceName]
				log.Printf("[HealthCheck] Service %s: found %d tasks with deployID %s", svc.ServiceName, len(tasks), deployID)
				hasRunningTask := false

				for _, t := range tasks {
					if t.Status.State == dockerswarm.TaskStateFailed ||
						t.Status.State == dockerswarm.TaskStateShutdown ||
						t.Status.State == dockerswarm.TaskStateRejected {
						log.Printf("[HealthCheck] ⚠️  Task %s (%s) is %s: %s (waiting for restart)", t.ID[:12], svc.ServiceName, t.Status.State, t.Status.Message)
						continue
					}

					if t.DesiredState != dockerswarm.TaskStateRunning {
						continue
					}
					hasRunningTask = true

					if t.Status.State != dockerswarm.TaskStateRunning {
						allHealthy = false
						unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s/%s (state: %s)", svc.ServiceName, t.ID[:12], t.Status.State))
						log.Printf("[HealthCheck] ⏳ Task %s (%s) is %s", t.ID[:12], svc.ServiceName, t.Status.State)
						continue
					}

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

			results := make([]inspectResult, len(containersToInspect))
			inspectGroup, inspectCtx := errgroup.WithContext(ctx)
			inspectGroup.SetLimit(inspectParallelism)

			for i, ct := range containersToInspect {
				i := i
				ct := ct
				inspectGroup.Go(func() error {
					defer recoverPanic("runContainerInspect")
					info, err := cli.ContainerInspect(inspectCtx, ct.containerID)
					results[i] = inspectResult{ct: ct, info: info, err: err}
					return nil
				})
			}

			if err := inspectGroup.Wait(); err != nil {
				return err
			}

			for _, result := range results {
				ct := result.ct
				taskID := ct.task.ID
				serviceName := ct.serviceName

				if result.err != nil {
					log.Printf("[HealthCheck] Failed to inspect container %s for task %s (%s): %v", ct.containerID[:12], taskID[:12], serviceName, result.err)
					allHealthy = false
					unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s/%s (inspect failed)", serviceName, taskID[:12]))
					continue
				}

				if result.info.State.Health != nil {
					if result.info.State.Health.Status != container.Healthy {
						allHealthy = false
						unhealthyTasks = append(unhealthyTasks, fmt.Sprintf("%s/%s (health: %s)", serviceName, taskID[:12], result.info.State.Health.Status))
						log.Printf("[HealthCheck] ⏳ Task %s (%s) is %s", taskID[:12], serviceName, result.info.State.Health.Status)
					} else {
						log.Printf("[HealthCheck] ✅ Task %s (%s) is healthy", taskID[:12], serviceName)
						serviceHealthyCount[serviceName]++
					}
				} else {
					log.Printf("[HealthCheck] ✅ Task %s (%s) is running (no healthcheck)", taskID[:12], serviceName)
					serviceHealthyCount[serviceName]++
				}
			}

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

// containerTask associates a container ID with its task and service info for health inspection.
type containerTask struct {
	containerID string
	task        dockerswarm.Task
	serviceName string
}

// inspectResult holds the result of a single container inspection.
type inspectResult struct {
	ct   containerTask
	info types.ContainerJSON
	err  error
}

// runSignalHandler waits for an OS signal, performs rollback and cancels the main context.
func runSignalHandler(
	ctx context.Context,
	sigChan <-chan os.Signal,
	deploymentComplete <-chan struct{},
	snap *swarm.StackSnapshot,
	deployer *swarm.StackDeployer,
	rollbackTimeout time.Duration,
	cancel context.CancelFunc,
	errCh chan<- error,
) {
	defer recoverPanic("runSignalHandler")

	select {
	case <-ctx.Done():
		return
	case <-deploymentComplete:
		return
	case sig := <-sigChan:
		log.Printf("Received signal: %v", sig)
		log.Println("Deployment interrupted, initiating rollback...")
		cancel()

		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer rollbackCancel()
		snapshot.Rollback(rollbackCtx, deployer, snap)
		nonBlockingSendError(errCh, fmt.Errorf("deployment interrupted by signal %v", sig))
	}
}

// runServiceUpdateMonitor waits for a service update to complete.
func runServiceUpdateMonitor(ctx context.Context, cli *client.Client, svc swarm.ServiceUpdateResult) error {
	defer recoverPanic("runServiceUpdateMonitor")

	updateMonitor := health.NewServiceUpdateMonitorWithLogger(cli, svc.ServiceID, svc.ServiceName, log.Default())
	if err := updateMonitor.WaitForUpdateComplete(ctx); err != nil {
		log.Printf("[ServiceUpdateMonitor] ❌ Service %s update failed: %v", svc.ServiceName, err)
		return fmt.Errorf("service %s update failed: %w", svc.ServiceName, err)
	}

	log.Printf("[ServiceUpdateMonitor] ✅ Service %s update completed successfully", svc.ServiceName)
	return nil
}

// runServiceWatcher runs a service event watcher until ctx is cancelled.
func runServiceWatcher(ctx context.Context, w *health.Watcher, svcName string) error {
	defer recoverPanic("runServiceWatcher")
	if err := w.Start(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("task watcher error for service %s: %w", svcName, err)
	}
	return nil
}

// runTaskMonitorWorker starts a task monitor and removes it from the registry when done.
func runTaskMonitorWorker(ctx context.Context, m *health.Monitor, taskID string, registry map[string]*health.Monitor, mu *sync.Mutex) {
	defer recoverPanic("runTaskMonitorWorker")

	if err := m.Start(ctx); err != nil && err != context.Canceled {
		log.Printf("[ServiceMonitor] Monitor error for task %s: %v", shortTaskID(taskID), err)
	}

	mu.Lock()
	delete(registry, taskID)
	mu.Unlock()

	log.Printf("[ServiceMonitor] Task %s monitor finished", shortTaskID(taskID))
}

func tryGetSignalError(errCh <-chan error) error {
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func nonBlockingSendError(errCh chan<- error, err error) {
	select {
	case errCh <- err:
	default:
	}
}

func recoverPanic(worker string) {
	if recovered := recover(); recovered != nil {
		log.Printf("[PanicRecover] %s recovered panic: %v\n%s", worker, recovered, string(debug.Stack()))
	}
}

// shortTaskID returns a safe 12-character prefix of a Docker task ID for logging.
func shortTaskID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
