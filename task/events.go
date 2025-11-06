package task

import (
	"time"
)

// EventType represents the type of task event
type EventType string

const (
	// EventTypeCreated is emitted when a new task is created
	EventTypeCreated EventType = "task_created"

	// EventTypeStarted is emitted when a task container starts
	EventTypeStarted EventType = "task_started"

	// EventTypeRunning is emitted when a task is confirmed running
	EventTypeRunning EventType = "task_running"

	// EventTypeHealthy is emitted when a task becomes healthy
	EventTypeHealthy EventType = "task_healthy"

	// EventTypeUnhealthy is emitted when a task becomes unhealthy
	EventTypeUnhealthy EventType = "task_unhealthy"

	// EventTypeFailed is emitted when a task fails
	EventTypeFailed EventType = "task_failed"

	// EventTypeCompleted is emitted when a task completes successfully
	EventTypeCompleted EventType = "task_completed"

	// EventTypeShutdown is emitted when a task is being shut down
	EventTypeShutdown EventType = "task_shutdown"
)

// Event represents a task lifecycle event
type Event struct {
	// Type of the event
	Type EventType

	// TaskID is the Docker Swarm task ID
	TaskID string

	// ServiceID is the Docker Swarm service ID
	ServiceID string

	// ServiceName is the human-readable service name
	ServiceName string

	// ContainerID is the container ID (if available)
	ContainerID string

	// NodeID is the node where task is running
	NodeID string

	// Timestamp when the event occurred
	Timestamp time.Time

	// State is the current task state (e.g., "running", "failed")
	State string

	// DesiredState is the desired task state (e.g., "running", "shutdown")
	DesiredState string

	// Message contains additional event details
	Message string

	// Error contains error information if event is failure-related
	Error error
}

// IsTerminal returns true if the event represents a terminal state
// (task will not transition to another state)
func (e *Event) IsTerminal() bool {
	return e.Type == EventTypeFailed ||
		e.Type == EventTypeCompleted ||
		e.Type == EventTypeShutdown
}

// IsHealthRelated returns true if the event is related to health checks
func (e *Event) IsHealthRelated() bool {
	return e.Type == EventTypeHealthy || e.Type == EventTypeUnhealthy
}

// IsFailure returns true if the event represents a failure
func (e *Event) IsFailure() bool {
	return e.Type == EventTypeFailed || e.Type == EventTypeUnhealthy
}
