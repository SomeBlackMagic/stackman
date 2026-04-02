package health

import "time"

// EventType represents a stack task lifecycle event.
type EventType string

const (
	EventTypeCreated   EventType = "task_created"
	EventTypeStarted   EventType = "task_started"
	EventTypeRunning   EventType = "task_running"
	EventTypeHealthy   EventType = "task_healthy"
	EventTypeUnhealthy EventType = "task_unhealthy"
	EventTypeFailed    EventType = "task_failed"
	EventTypeShutdown  EventType = "task_shutdown"
)

// Event is a normalized health event derived from Docker task/container events.
type Event struct {
	Type        EventType
	TaskID      string
	ServiceID   string
	ServiceName string
	ContainerID string
	NodeID      string
	Timestamp   time.Time
	Message     string
	Error       string
}

// IsTerminal returns true when the event ends a task lifecycle.
func (e Event) IsTerminal() bool {
	switch e.Type {
	case EventTypeHealthy, EventTypeFailed, EventTypeShutdown:
		return true
	default:
		return false
	}
}

// IsFailure returns true when the event indicates an unhealthy or failed state.
func (e Event) IsFailure() bool {
	switch e.Type {
	case EventTypeFailed, EventTypeUnhealthy:
		return true
	default:
		return false
	}
}
