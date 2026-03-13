package health

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
)

// Watcher monitors Docker events and emits task lifecycle events
type Watcher struct {
	client    DockerClient
	logger    Logger
	stackName string

	// Optional: filter by specific service, version and deployID
	filterServiceID string
	filterVersion   uint64
	filterDeployID  string

	// eventChan broadcasts task events to subscribers
	eventChan chan Event

	// subscribers holds all active event subscribers
	subscribers   []chan Event
	subscribersMu sync.RWMutex

	// taskStates tracks current state of tasks
	taskStates   map[string]*taskState
	taskStatesMu sync.RWMutex

	// existingTasks tracks tasks that existed before monitoring started
	// We don't emit events for these to avoid noise from already-running containers
	existingTasks   map[string]bool
	existingTasksMu sync.RWMutex

	// serviceNames maps service IDs to names
	serviceNames   map[string]string
	serviceNamesMu sync.RWMutex

	// shutdown management
	shutdownOnce sync.Once
	workersWG    sync.WaitGroup
	done         chan struct{}
}

// taskState tracks the internal state of a task
type taskState struct {
	taskID       string
	serviceID    string
	serviceName  string
	containerID  string
	nodeID       string
	state        string
	desiredState string
	lastSeen     time.Time
}

// NewWatcher creates a new task event watcher for entire stack
func NewWatcher(client DockerClient, stackName string) *Watcher {
	return NewWatcherWithLogger(client, stackName, nil)
}

// NewWatcherWithLogger creates a new task event watcher for entire stack with custom logger.
func NewWatcherWithLogger(client DockerClient, stackName string, logger Logger) *Watcher {
	return &Watcher{
		client:        client,
		logger:        withDefaultLogger(logger),
		stackName:     stackName,
		eventChan:     make(chan Event, 100), // buffered to avoid blocking
		subscribers:   make([]chan Event, 0),
		taskStates:    make(map[string]*taskState),
		existingTasks: make(map[string]bool),
		serviceNames:  make(map[string]string),
		done:          make(chan struct{}),
	}
}

// NewServiceWatcher creates a task watcher filtered for specific service, version and deployID
// Only tasks with version >= minVersion AND matching deployID for this serviceID will emit events
func NewServiceWatcher(client DockerClient, stackName string, serviceID string, minVersion uint64, deployID string) *Watcher {
	return NewServiceWatcherWithLogger(client, stackName, serviceID, minVersion, deployID, nil)
}

// NewServiceWatcherWithLogger creates a task watcher with custom logger.
func NewServiceWatcherWithLogger(client DockerClient, stackName string, serviceID string, minVersion uint64, deployID string, logger Logger) *Watcher {
	return &Watcher{
		client:          client,
		logger:          withDefaultLogger(logger),
		stackName:       stackName,
		filterServiceID: serviceID,
		filterVersion:   minVersion,
		filterDeployID:  deployID,
		eventChan:       make(chan Event, 100),
		subscribers:     make([]chan Event, 0),
		taskStates:      make(map[string]*taskState),
		existingTasks:   make(map[string]bool),
		serviceNames:    make(map[string]string),
		done:            make(chan struct{}),
	}
}

// Start begins watching for task events
// This method blocks until context is cancelled
func (w *Watcher) Start(ctx context.Context) error {
	// Initialize service name cache
	if err := w.loadServiceNames(ctx); err != nil {
		w.logf("Warning: failed to load service names: %v", err)
	}

	// Scan and mark existing tasks before starting monitoring
	// This prevents emitting events for already-running containers
	if err := w.markExistingTasks(ctx); err != nil {
		w.logf("Warning: failed to scan existing tasks: %v", err)
	}

	// Start event broadcaster and periodic polling workers.
	w.workersWG.Add(2)
	go w.runWorker(ctx, "broadcastEvents", w.broadcastEvents)
	go w.runWorker(ctx, "pollTasks", w.pollTasks)

	// Subscribe to Docker events
	// NOTE: Docker Swarm does NOT emit "task" type events through Events API
	// We derive task lifecycle from container and service events
	eventFilter := filters.NewArgs()
	eventFilter.Add("type", "service")
	eventFilter.Add("type", "container")

	eventsChan, errChan := w.client.Events(ctx, events.ListOptions{
		Filters: eventFilter,
	})

	w.logf("[TaskWatcher] Started watching events for stack: %s", w.stackName)
	defer w.workersWG.Wait()

	for {
		select {
		case <-ctx.Done():
			w.logf("[TaskWatcher] Context cancelled, stopping watcher")
			w.shutdown()
			return ctx.Err()

		case err := <-errChan:
			if err != nil {
				w.logf("[TaskWatcher] Error receiving events: %v", err)
				w.shutdown()
				return err
			}

		case dockerEvent := <-eventsChan:
			w.handleDockerEvent(ctx, dockerEvent)
		}
	}
}

// Subscribe returns a channel that receives task events
// The returned channel should be consumed continuously to avoid blocking
// Call Unsubscribe when done to clean up resources
func (w *Watcher) Subscribe() <-chan Event {
	w.subscribersMu.Lock()
	defer w.subscribersMu.Unlock()

	ch := make(chan Event, 50) // buffered to reduce blocking
	w.subscribers = append(w.subscribers, ch)
	return ch
}

// Unsubscribe removes a subscriber channel
// The channel will be closed automatically when the Watcher shuts down
func (w *Watcher) Unsubscribe(ch <-chan Event) {
	w.subscribersMu.Lock()
	defer w.subscribersMu.Unlock()

	for i, sub := range w.subscribers {
		if sub == ch {
			// Don't close here to avoid race condition with broadcastEvents
			// The broadcaster will close all channels on shutdown
			w.subscribers = append(w.subscribers[:i], w.subscribers[i+1:]...)
			break
		}
	}
}

// handleDockerEvent processes Docker events and converts them to task events
func (w *Watcher) handleDockerEvent(ctx context.Context, dockerEvent events.Message) {
	// Filter by stack name
	if !w.belongsToStack(dockerEvent) {
		return
	}

	// TODO Debug: log all events for this stack
	//w.logf("[TaskWatcher] DEBUG: Received event Type=%s Action=%s Actor.ID=%s ServiceID=%s",
	//	dockerEvent.Type, dockerEvent.Action, dockerEvent.Actor.ID[:min(12, len(dockerEvent.Actor.ID))],
	//	dockerEvent.Actor.Attributes["com.docker.swarm.service.id"])

	switch dockerEvent.Type {
	case "task":
		w.handleTaskEvent(ctx, dockerEvent)
	case "container":
		w.handleContainerEvent(ctx, dockerEvent)
	case "service":
		w.handleServiceEvent(ctx, dockerEvent)
	default:
		// Log unknown event types for debugging
		w.logf("[TaskWatcher] DEBUG: Unknown event type: %s (action: %s)",
			dockerEvent.Type, dockerEvent.Action)
	}
}

// handleTaskEvent processes task-related Docker events
func (w *Watcher) handleTaskEvent(ctx context.Context, dockerEvent events.Message) {
	taskID := dockerEvent.Actor.ID
	serviceID := dockerEvent.Actor.Attributes["com.docker.swarm.service.id"]
	serviceName := dockerEvent.Actor.Attributes["com.docker.swarm.service.name"]
	nodeID := dockerEvent.Actor.Attributes["com.docker.swarm.node.id"]
	containerID := dockerEvent.Actor.Attributes["container"]

	// Update task state cache
	w.taskStatesMu.Lock()
	state, exists := w.taskStates[taskID]
	if !exists {
		state = &taskState{
			taskID:      taskID,
			serviceID:   serviceID,
			serviceName: serviceName,
			containerID: containerID,
			nodeID:      nodeID,
		}
		w.taskStates[taskID] = state
	}
	state.lastSeen = time.Now()
	w.taskStatesMu.Unlock()

	var eventType EventType
	var message string

	// Map container events to task lifecycle events
	switch dockerEvent.Action {
	case "create":
		eventType = EventTypeCreated
		message = "Task created"

	case "start":
		eventType = EventTypeStarted
		message = "Task started"

	case "running":
		eventType = EventTypeRunning
		message = "Task running"

	case "complete":
		eventType = EventTypeCompleted
		message = "Task completed"

	case "failed", "reject":
		eventType = EventTypeFailed
		message = fmt.Sprintf("Task failed: %s", dockerEvent.Action)

	case "shutdown", "remove":
		eventType = EventTypeShutdown
		message = fmt.Sprintf("Task %s", dockerEvent.Action)

	default:
		// Ignore unknown task events
		return
	}

	w.emitEvent(Event{
		Type:         eventType,
		TaskID:       taskID,
		ServiceID:    serviceID,
		ServiceName:  serviceName,
		ContainerID:  containerID,
		NodeID:       nodeID,
		Timestamp:    time.Unix(dockerEvent.Time, 0),
		State:        string(dockerEvent.Action),
		DesiredState: dockerEvent.Actor.Attributes["desired-state"],
		Message:      message,
	})
}

// handleContainerEvent processes container-related Docker events
// Since Docker Swarm doesn't emit task events, we derive task lifecycle from container events
func (w *Watcher) handleContainerEvent(ctx context.Context, dockerEvent events.Message) {
	// Extract task ID from container name or labels
	taskID := dockerEvent.Actor.Attributes["com.docker.swarm.task.id"]
	if taskID == "" {
		return // Not a swarm task container
	}

	serviceID := dockerEvent.Actor.Attributes["com.docker.swarm.service.id"]
	serviceName := dockerEvent.Actor.Attributes["com.docker.swarm.service.name"]
	nodeID := dockerEvent.Actor.Attributes["com.docker.swarm.node.id"]
	containerID := dockerEvent.Actor.ID

	// If watcher is filtered for specific service, check it
	if w.filterServiceID != "" && serviceID != w.filterServiceID {
		return // Not our service
	}

	// If watcher is filtered by version or deployID, check task
	if w.filterVersion > 0 || w.filterDeployID != "" {
		task, err := w.InspectTask(ctx, taskID)
		if err != nil {
			// Can't get task info, log and skip
			w.logf("[TaskWatcher] WARNING: Failed to inspect task %s for version/deployID check: %v", shortID(taskID), err)
			return
		}

		// Check version filter
		if w.filterVersion > 0 && task.Version.Index < w.filterVersion {
			// TODO Debug logs
			//w.logf("[TaskWatcher] Skipping task %s (version %d < %d)", taskID[:12], task.Version.Index, w.filterVersion)
			return
		}

		// Check deployID filter
		if w.filterDeployID != "" {
			taskDeployID := ""
			if task.Spec.ContainerSpec != nil && task.Spec.ContainerSpec.Labels != nil {
				taskDeployID = task.Spec.ContainerSpec.Labels["com.stackman.deploy.id"]
			}

			if taskDeployID != w.filterDeployID {
				// TODO Debug logs
				//w.logf("[TaskWatcher] Skipping task %s (deployID '%s' != '%s')", taskID[:12], taskDeployID, w.filterDeployID)
				return
			}
		}

		// Log only once per task (on create event)
		if dockerEvent.Action == "create" {
			w.logf("[TaskWatcher] Processing task %s (version %d >= %d, deployID: %s)",
				shortID(taskID), task.Version.Index, w.filterVersion, w.filterDeployID)
		}
	}

	// Update or create task state
	w.taskStatesMu.Lock()
	state, exists := w.taskStates[taskID]
	if !exists {
		state = &taskState{
			taskID:      taskID,
			serviceID:   serviceID,
			serviceName: serviceName,
			containerID: containerID,
			nodeID:      nodeID,
		}
		w.taskStates[taskID] = state
	}
	state.containerID = containerID
	state.lastSeen = time.Now()
	w.taskStatesMu.Unlock()

	var eventType EventType
	var message string

	// Map container events to task lifecycle events
	switch dockerEvent.Action {
	case "create":
		eventType = EventTypeCreated
		message = "Task container created"

	case "start":
		eventType = EventTypeStarted
		message = "Task container started"

	case "die":
		// Determine if task failed or completed based on exit code
		exitCode := dockerEvent.Actor.Attributes["exitCode"]
		if exitCode == "0" {
			eventType = EventTypeCompleted
			message = "Task completed successfully"
		} else {
			eventType = EventTypeFailed
			message = fmt.Sprintf("Task failed with exit code %s", exitCode)
		}

	case "kill", "stop":
		eventType = EventTypeShutdown
		message = "Task is being shut down"

	case "health_status":
		// Check health status from attributes
		healthStatus := dockerEvent.Actor.Attributes["health_status"]
		switch healthStatus {
		case "healthy":
			eventType = EventTypeHealthy
			message = "Container is healthy"
		case "unhealthy":
			eventType = EventTypeUnhealthy
			message = "Container is unhealthy"
		default:
			// Unknown health status, ignore
			return
		}

	case "destroy":
		// Container was removed - task is fully terminated
		eventType = EventTypeShutdown
		message = "Task container destroyed"

	default:
		// Ignore other container events (exec_*, attach, etc)
		return
	}

	w.emitEvent(Event{
		Type:        eventType,
		TaskID:      taskID,
		ServiceID:   serviceID,
		ServiceName: serviceName,
		ContainerID: containerID,
		NodeID:      nodeID,
		Timestamp:   time.Unix(dockerEvent.Time, 0),
		State:       string(dockerEvent.Action),
		Message:     message,
	})
}

// handleServiceEvent processes service-related Docker events
func (w *Watcher) handleServiceEvent(ctx context.Context, dockerEvent events.Message) {
	serviceID := dockerEvent.Actor.ID
	serviceName := dockerEvent.Actor.Attributes["name"]

	// Update service name cache
	w.serviceNamesMu.Lock()
	w.serviceNames[serviceID] = serviceName
	w.serviceNamesMu.Unlock()
}

// emitEvent sends an event to the internal event channel
func (w *Watcher) emitEvent(event Event) {
	select {
	case w.eventChan <- event:
		// Event queued successfully
	case <-w.done:
		// Watcher is shutting down
	default:
		// Event channel is full, log warning
		w.logf("[TaskWatcher] WARNING: event channel full, dropping event: %s for task %s",
			event.Type, event.TaskID)
	}
}

// broadcastEvents distributes events from internal channel to all subscribers
func (w *Watcher) broadcastEvents(ctx context.Context) {
	defer func() {
		// Close all subscriber channels on shutdown
		w.subscribersMu.Lock()
		for _, ch := range w.subscribers {
			close(ch)
		}
		w.subscribers = nil
		w.subscribersMu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.done:
			return
		case event := <-w.eventChan:
			w.subscribersMu.RLock()
			for _, ch := range w.subscribers {
				select {
				case ch <- event:
					// Event sent successfully
				default:
					// Subscriber is slow, log warning
					w.logf("[TaskWatcher] WARNING: subscriber slow, dropping event for task %s",
						event.TaskID)
				}
			}
			w.subscribersMu.RUnlock()
		}
	}
}

// belongsToStack checks if a Docker event belongs to this stack
func (w *Watcher) belongsToStack(dockerEvent events.Message) bool {
	// Check service name for stack prefix
	if serviceName, ok := dockerEvent.Actor.Attributes["com.docker.swarm.service.name"]; ok {
		return strings.HasPrefix(serviceName, w.stackName+"_")
	}

	// Check container labels for stack name
	if stackLabel, ok := dockerEvent.Actor.Attributes["com.docker.stack.namespace"]; ok {
		return stackLabel == w.stackName
	}

	// For service events, check name directly
	if name, ok := dockerEvent.Actor.Attributes["name"]; ok {
		return strings.HasPrefix(name, w.stackName+"_")
	}

	return false
}

// loadServiceNames fetches current service names and caches them
func (w *Watcher) loadServiceNames(ctx context.Context) error {
	filter := filters.NewArgs()
	filter.Add("label", "com.docker.stack.namespace="+w.stackName)

	services, err := w.client.ServiceList(ctx, types.ServiceListOptions{
		Filters: filter,
	})
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	w.serviceNamesMu.Lock()
	defer w.serviceNamesMu.Unlock()

	for _, service := range services {
		w.serviceNames[service.ID] = service.Spec.Name
	}

	return nil
}

// GetTaskState returns current state of a task (if tracked)
func (w *Watcher) GetTaskState(taskID string) (state string, desiredState string, exists bool) {
	w.taskStatesMu.RLock()
	defer w.taskStatesMu.RUnlock()

	if ts, ok := w.taskStates[taskID]; ok {
		return ts.state, ts.desiredState, true
	}
	return "", "", false
}

// GetTrackedTasks returns all currently tracked task IDs
func (w *Watcher) GetTrackedTasks() []string {
	w.taskStatesMu.RLock()
	defer w.taskStatesMu.RUnlock()

	tasks := make([]string, 0, len(w.taskStates))
	for taskID := range w.taskStates {
		tasks = append(tasks, taskID)
	}
	return tasks
}

// CleanupOldTasks removes task states that haven't been seen recently
// This prevents memory leaks from accumulating old task data
func (w *Watcher) CleanupOldTasks(maxAge time.Duration) int {
	w.taskStatesMu.Lock()
	defer w.taskStatesMu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for taskID, state := range w.taskStates {
		if state.lastSeen.Before(cutoff) {
			delete(w.taskStates, taskID)
			removed++
		}
	}

	if removed > 0 {
		w.logf("[TaskWatcher] Cleaned up %d old task states", removed)
	}

	return removed
}

// shutdown performs graceful shutdown of the watcher.
// eventChan is intentionally not closed here: broadcastEvents exits via <-w.done,
// and closing eventChan after done creates a race where broadcastEvents could read
// a zero-value Event from the closed channel before seeing the done signal.
func (w *Watcher) shutdown() {
	w.shutdownOnce.Do(func() {
		w.logf("[TaskWatcher] Shutting down")
		close(w.done)
	})
}

func (w *Watcher) logf(format string, args ...any) {
	w.logger.Printf(format, args...)
}

func (w *Watcher) runWorker(ctx context.Context, name string, fn func(context.Context)) {
	defer w.workersWG.Done()
	defer func() {
		if recovered := recover(); recovered != nil {
			w.logf("[TaskWatcher] panic recovered in %s: %v\n%s", name, recovered, string(debug.Stack()))
			w.shutdown()
		}
	}()
	fn(ctx)
}

// InspectTask fetches detailed task information from Docker API
func (w *Watcher) InspectTask(ctx context.Context, taskID string) (*swarm.Task, error) {
	// Use TaskList with filter to get specific task
	filter := filters.NewArgs()
	filter.Add("id", taskID)

	tasks, err := w.client.TaskList(ctx, types.TaskListOptions{
		Filters: filter,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect task %s: %w", taskID, err)
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	return &tasks[0], nil
}

// GetTasksForService returns all currently tracked tasks for a specific service
func (w *Watcher) GetTasksForService(serviceID string) []string {
	w.taskStatesMu.RLock()
	defer w.taskStatesMu.RUnlock()

	tasks := make([]string, 0)
	for taskID, state := range w.taskStates {
		if state.serviceID == serviceID {
			tasks = append(tasks, taskID)
		}
	}
	return tasks
}

// SubscribeToService creates a filtered channel that only receives events for a specific service.
// ctx controls the lifetime of the filter goroutine independently of the watcher lifetime.
// Returns the filtered channel and an unsubscribe function that MUST be called to stop the goroutine.
func (w *Watcher) SubscribeToService(ctx context.Context, serviceID string) (<-chan Event, func()) {
	allEvents := w.Subscribe()
	filtered := make(chan Event, 50)
	done := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)
		w.runServiceEventFilter(ctx, serviceID, allEvents, filtered, done)
	}()

	return filtered, func() {
		close(done)
		<-stopped
	}
}

// runServiceEventFilter forwards events matching serviceID from allEvents to filtered.
// Exits when done is closed, ctx is cancelled, or allEvents is closed.
func (w *Watcher) runServiceEventFilter(ctx context.Context, serviceID string, allEvents <-chan Event, filtered chan<- Event, done <-chan struct{}) {
	defer close(filtered)
	defer w.Unsubscribe(allEvents)

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case event, ok := <-allEvents:
			if !ok {
				return
			}
			if event.ServiceID == serviceID {
				select {
				case filtered <- event:
				case <-ctx.Done():
					return
				case <-done:
					return
				default:
					w.logf("[TaskWatcher] WARNING: Filtered channel full for service %s", serviceID)
				}
			}
		}
	}
}

// pollTasks periodically polls Task API to discover new tasks.
// This complements event-based monitoring by catching tasks created before subscription.
// It also runs periodic cleanup of stale taskStates entries to prevent unbounded growth.
func (w *Watcher) pollTasks(ctx context.Context) {
	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()

	cleanupTicker := time.NewTicker(5 * time.Minute)
	defer cleanupTicker.Stop()

	w.logf("[TaskWatcher] Started periodic task polling")

	for {
		select {
		case <-ctx.Done():
			w.logf("[TaskWatcher] Task polling stopped")
			return
		case <-pollTicker.C:
			w.discoverTasks(ctx)
		case <-cleanupTicker.C:
			w.CleanupOldTasks(10 * time.Minute)
		}
	}
}

// discoverTasks fetches tasks from Docker API and emits events for new ones
func (w *Watcher) discoverTasks(ctx context.Context) {
	// Build filter for tasks
	filter := filters.NewArgs()
	filter.Add("label", "com.docker.stack.namespace="+w.stackName)

	// If watching specific service, filter by service ID
	if w.filterServiceID != "" {
		filter.Add("service", w.filterServiceID)
	}

	tasks, err := w.client.TaskList(ctx, swarm.TaskListOptions{
		Filters: filter,
	})
	if err != nil {
		w.logf("[TaskWatcher] Warning: failed to poll tasks: %v", err)
		return
	}

	for _, task := range tasks {
		// Apply version filter if set
		if w.filterVersion > 0 && task.Version.Index < w.filterVersion {
			continue
		}

		// Apply deployID filter if set
		if w.filterDeployID != "" {
			taskDeployID := ""
			if task.Spec.ContainerSpec != nil && task.Spec.ContainerSpec.Labels != nil {
				taskDeployID = task.Spec.ContainerSpec.Labels["com.stackman.deploy.id"]
			}
			if taskDeployID != w.filterDeployID {
				continue
			}
		}

		// Skip existing tasks (those that existed before monitoring started).
		// Remove terminal tasks from the map so it does not grow unboundedly.
		w.existingTasksMu.RLock()
		isExisting := w.existingTasks[task.ID]
		w.existingTasksMu.RUnlock()

		if isExisting {
			if isTaskTerminal(task.Status.State) {
				w.existingTasksMu.Lock()
				delete(w.existingTasks, task.ID)
				w.existingTasksMu.Unlock()
			}
			continue
		}

		// Check if this is a new task
		w.taskStatesMu.RLock()
		_, exists := w.taskStates[task.ID]
		w.taskStatesMu.RUnlock()

		if !exists {
			// New task discovered! Emit creation event
			w.logf("[TaskWatcher] Discovered new task via polling: %s (service: %s, version: %d, state: %s)",
				shortID(task.ID), shortID(task.ServiceID), task.Version.Index, task.Status.State)

			// Create task state
			containerID := ""
			if task.Status.ContainerStatus != nil {
				containerID = task.Status.ContainerStatus.ContainerID
			}

			svcName := ""
			if task.Spec.ContainerSpec != nil && task.Spec.ContainerSpec.Labels != nil {
				svcName = task.Spec.ContainerSpec.Labels["com.docker.swarm.service.name"]
			}

			w.taskStatesMu.Lock()
			w.taskStates[task.ID] = &taskState{
				taskID:       task.ID,
				serviceID:    task.ServiceID,
				serviceName:  svcName,
				containerID:  containerID,
				state:        string(task.Status.State),
				desiredState: string(task.DesiredState),
				lastSeen:     time.Now(),
			}
			w.taskStatesMu.Unlock()

			// Emit created event
			w.emitEvent(Event{
				Type:        EventTypeCreated,
				TaskID:      task.ID,
				ServiceID:   task.ServiceID,
				ServiceName: w.getServiceName(task.ServiceID),
				ContainerID: containerID,
				State:       string(task.Status.State),
				Message:     "Task discovered via polling",
				Timestamp:   time.Now(),
			})

			// If task already has container ID, emit container info
			if containerID != "" {
				w.emitEvent(Event{
					Type:        EventTypeStarted,
					TaskID:      task.ID,
					ServiceID:   task.ServiceID,
					ServiceName: w.getServiceName(task.ServiceID),
					ContainerID: containerID,
					State:       string(task.Status.State),
					Message:     fmt.Sprintf("Task in state: %s", task.Status.State),
					Timestamp:   time.Now(),
				})
			}
		}
	}
}

// getServiceName retrieves service name from cache
func (w *Watcher) getServiceName(serviceID string) string {
	w.serviceNamesMu.RLock()
	defer w.serviceNamesMu.RUnlock()

	if name, ok := w.serviceNames[serviceID]; ok {
		return name
	}
	return shortID(serviceID)
}

// shortID returns a safe 12-character prefix of a Docker ID for logging.
// Handles IDs shorter than 12 characters without panicking.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// isTaskTerminal reports whether a task state is terminal,
// meaning no further state transitions are expected.
func isTaskTerminal(state swarm.TaskState) bool {
	switch state {
	case swarm.TaskStateComplete, swarm.TaskStateFailed,
		swarm.TaskStateShutdown, swarm.TaskStateRejected:
		return true
	}
	return false
}

// markExistingTasks scans and marks all currently running tasks
// This prevents emitting events for tasks that existed before monitoring started
func (w *Watcher) markExistingTasks(ctx context.Context) error {
	// Scan existing containers and extract their task IDs
	containerFilter := filters.NewArgs()
	containerFilter.Add("label", "com.docker.stack.namespace="+w.stackName)

	// If watching specific service, filter by service ID
	if w.filterServiceID != "" {
		containerFilter.Add("label", "com.docker.swarm.service.id="+w.filterServiceID)
	}

	containers, err := w.client.ContainerList(ctx, container.ListOptions{
		All:     false, // Only running containers
		Filters: containerFilter,
	})
	if err != nil {
		return fmt.Errorf("failed to list existing containers: %w", err)
	}

	w.existingTasksMu.Lock()
	defer w.existingTasksMu.Unlock()

	// Mark task IDs from existing containers
	for _, c := range containers {
		taskID := c.Labels["com.docker.swarm.task.id"]
		if taskID != "" {
			w.existingTasks[taskID] = true
			w.logf("[TaskWatcher] Marked task %s as existing (container %s)",
				shortID(taskID), shortID(c.ID))
		}
	}

	w.logf("[TaskWatcher] Marked %d existing task(s) to ignore their events", len(w.existingTasks))
	return nil
}
