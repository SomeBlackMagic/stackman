package task

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

// Watcher monitors Docker events and emits task lifecycle events
type Watcher struct {
	client    client.APIClient
	stackName string

	// eventChan broadcasts task events to subscribers
	eventChan chan Event

	// subscribers holds all active event subscribers
	subscribers   []chan Event
	subscribersMu sync.RWMutex

	// taskStates tracks current state of tasks
	taskStates   map[string]*taskState
	taskStatesMu sync.RWMutex

	// serviceNames maps service IDs to names
	serviceNames   map[string]string
	serviceNamesMu sync.RWMutex

	// shutdown management
	shutdownOnce sync.Once
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

// NewWatcher creates a new task event watcher
func NewWatcher(client client.APIClient, stackName string) *Watcher {
	return &Watcher{
		client:       client,
		stackName:    stackName,
		eventChan:    make(chan Event, 100), // buffered to avoid blocking
		subscribers:  make([]chan Event, 0),
		taskStates:   make(map[string]*taskState),
		serviceNames: make(map[string]string),
		done:         make(chan struct{}),
	}
}

// Start begins watching for task events
// This method blocks until context is cancelled
func (w *Watcher) Start(ctx context.Context) error {
	// Initialize service name cache
	if err := w.loadServiceNames(ctx); err != nil {
		log.Printf("Warning: failed to load service names: %v", err)
	}

	// Start event broadcaster
	go w.broadcastEvents(ctx)

	// Subscribe to Docker events
	// NOTE: Docker Swarm does NOT emit "task" type events through Events API
	// We derive task lifecycle from container and service events
	eventFilter := filters.NewArgs()
	eventFilter.Add("type", "service")
	eventFilter.Add("type", "container")

	eventsChan, errChan := w.client.Events(ctx, events.ListOptions{
		Filters: eventFilter,
	})

	log.Printf("[TaskWatcher] Started watching events for stack: %s", w.stackName)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[TaskWatcher] Context cancelled, stopping watcher")
			w.shutdown()
			return ctx.Err()

		case err := <-errChan:
			if err != nil {
				log.Printf("[TaskWatcher] Error receiving events: %v", err)
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
func (w *Watcher) Unsubscribe(ch <-chan Event) {
	w.subscribersMu.Lock()
	defer w.subscribersMu.Unlock()

	for i, sub := range w.subscribers {
		if sub == ch {
			close(sub)
			w.subscribers = append(w.subscribers[:i], w.subscribers[i+1:]...)
			break
		}
	}
}

// handleDockerEvent processes Docker events and converts them to task events
func (w *Watcher) handleDockerEvent(ctx context.Context, dockerEvent events.Message) {
	// Debug: log all events for this stack
	if w.belongsToStack(dockerEvent) {
		log.Printf("[TaskWatcher] DEBUG: Received event Type=%s Action=%s Actor.ID=%s",
			dockerEvent.Type, dockerEvent.Action, dockerEvent.Actor.ID[:12])
	}

	// Filter by stack name
	if !w.belongsToStack(dockerEvent) {
		return
	}

	switch dockerEvent.Type {
	case "task":
		w.handleTaskEvent(ctx, dockerEvent)
	case "container":
		w.handleContainerEvent(ctx, dockerEvent)
	case "service":
		w.handleServiceEvent(ctx, dockerEvent)
	default:
		// Log unknown event types for debugging
		log.Printf("[TaskWatcher] DEBUG: Unknown event type: %s (action: %s)",
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

	case "health_status: healthy":
		eventType = EventTypeHealthy
		message = "Container is healthy"

	case "health_status: unhealthy":
		eventType = EventTypeUnhealthy
		message = "Container is unhealthy"

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
		log.Printf("[TaskWatcher] WARNING: event channel full, dropping event: %s for task %s",
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
					log.Printf("[TaskWatcher] WARNING: subscriber slow, dropping event for task %s",
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
		log.Printf("[TaskWatcher] Cleaned up %d old task states", removed)
	}

	return removed
}

// shutdown performs graceful shutdown of the watcher
func (w *Watcher) shutdown() {
	w.shutdownOnce.Do(func() {
		log.Printf("[TaskWatcher] Shutting down")
		close(w.done)
		close(w.eventChan)
	})
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

// SubscribeToService creates a filtered channel that only receives events for a specific service
func (w *Watcher) SubscribeToService(serviceID string) <-chan Event {
	// Subscribe to all events
	allEvents := w.Subscribe()

	// Create filtered channel
	filtered := make(chan Event, 50)

	// Start filter goroutine
	go func() {
		defer close(filtered)
		for event := range allEvents {
			if event.ServiceID == serviceID {
				select {
				case filtered <- event:
					// Event sent
				default:
					// Channel full, drop event
					log.Printf("[TaskWatcher] WARNING: Filtered channel full for service %s", serviceID)
				}
			}
		}
	}()

	return filtered
}
