package task

import (
	"testing"
	"time"
)

func TestEvent_IsTerminal(t *testing.T) {
	tests := []struct {
		name      string
		eventType EventType
		want      bool
	}{
		{"Failed is terminal", EventTypeFailed, true},
		{"Completed is terminal", EventTypeCompleted, true},
		{"Shutdown is terminal", EventTypeShutdown, true},
		{"Created is not terminal", EventTypeCreated, false},
		{"Running is not terminal", EventTypeRunning, false},
		{"Healthy is not terminal", EventTypeHealthy, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Event{Type: tt.eventType}
			if got := e.IsTerminal(); got != tt.want {
				t.Errorf("Event.IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvent_IsHealthRelated(t *testing.T) {
	tests := []struct {
		name      string
		eventType EventType
		want      bool
	}{
		{"Healthy is health-related", EventTypeHealthy, true},
		{"Unhealthy is health-related", EventTypeUnhealthy, true},
		{"Failed is not health-related", EventTypeFailed, false},
		{"Running is not health-related", EventTypeRunning, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Event{Type: tt.eventType}
			if got := e.IsHealthRelated(); got != tt.want {
				t.Errorf("Event.IsHealthRelated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvent_IsFailure(t *testing.T) {
	tests := []struct {
		name      string
		eventType EventType
		want      bool
	}{
		{"Failed is failure", EventTypeFailed, true},
		{"Unhealthy is failure", EventTypeUnhealthy, true},
		{"Completed is not failure", EventTypeCompleted, false},
		{"Healthy is not failure", EventTypeHealthy, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Event{Type: tt.eventType}
			if got := e.IsFailure(); got != tt.want {
				t.Errorf("Event.IsFailure() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEventCreation(t *testing.T) {
	now := time.Now()
	event := Event{
		Type:         EventTypeCreated,
		TaskID:       "task123",
		ServiceID:    "service456",
		ServiceName:  "mystack_web",
		ContainerID:  "container789",
		NodeID:       "node001",
		Timestamp:    now,
		State:        "new",
		DesiredState: "running",
		Message:      "Task created",
	}

	if event.Type != EventTypeCreated {
		t.Errorf("Expected event type %s, got %s", EventTypeCreated, event.Type)
	}

	if event.TaskID != "task123" {
		t.Errorf("Expected task ID 'task123', got '%s'", event.TaskID)
	}

	if event.ServiceName != "mystack_web" {
		t.Errorf("Expected service name 'mystack_web', got '%s'", event.ServiceName)
	}

	if !event.Timestamp.Equal(now) {
		t.Errorf("Expected timestamp %v, got %v", now, event.Timestamp)
	}
}
