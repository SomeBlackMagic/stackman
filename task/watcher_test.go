package task

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"
)

func TestNewWatcher(t *testing.T) {
	watcher := NewWatcher(nil, "teststack")

	if watcher == nil {
		t.Fatal("Expected watcher to be created")
	}

	if watcher.stackName != "teststack" {
		t.Errorf("Expected stack name 'teststack', got '%s'", watcher.stackName)
	}

	if watcher.taskStates == nil {
		t.Error("Expected taskStates map to be initialized")
	}

	if watcher.serviceNames == nil {
		t.Error("Expected serviceNames map to be initialized")
	}

	if watcher.eventChan == nil {
		t.Error("Expected eventChan to be initialized")
	}
}

func TestWatcher_Subscribe_Unsubscribe(t *testing.T) {
	watcher := NewWatcher(nil, "teststack")

	// Subscribe
	ch1 := watcher.Subscribe()
	ch2 := watcher.Subscribe()

	if len(watcher.subscribers) != 2 {
		t.Errorf("Expected 2 subscribers, got %d", len(watcher.subscribers))
	}

	// Unsubscribe
	watcher.Unsubscribe(ch1)

	if len(watcher.subscribers) != 1 {
		t.Errorf("Expected 1 subscriber after unsubscribe, got %d", len(watcher.subscribers))
	}

	// Verify channel is closed
	_, ok := <-ch1
	if ok {
		t.Error("Expected unsubscribed channel to be closed")
	}

	// ch2 should still be valid
	select {
	case _, ok := <-ch2:
		if !ok {
			t.Error("Expected ch2 to still be open")
		}
	default:
		// Channel is open and empty, which is correct
	}
}

func TestWatcher_BelongsToStack(t *testing.T) {
	watcher := NewWatcher(nil, "mystack")

	tests := []struct {
		name  string
		event events.Message
		want  bool
	}{
		{
			name: "Service with stack prefix",
			event: events.Message{
				Actor: events.Actor{
					Attributes: map[string]string{
						"com.docker.swarm.service.name": "mystack_web",
					},
				},
			},
			want: true,
		},
		{
			name: "Service without stack prefix",
			event: events.Message{
				Actor: events.Actor{
					Attributes: map[string]string{
						"com.docker.swarm.service.name": "otherstack_web",
					},
				},
			},
			want: false,
		},
		{
			name: "Container with stack namespace",
			event: events.Message{
				Actor: events.Actor{
					Attributes: map[string]string{
						"com.docker.stack.namespace": "mystack",
					},
				},
			},
			want: true,
		},
		{
			name: "Container with different namespace",
			event: events.Message{
				Actor: events.Actor{
					Attributes: map[string]string{
						"com.docker.stack.namespace": "otherstack",
					},
				},
			},
			want: false,
		},
		{
			name: "Service name matches",
			event: events.Message{
				Actor: events.Actor{
					Attributes: map[string]string{
						"name": "mystack_api",
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := watcher.belongsToStack(tt.event)
			if got != tt.want {
				t.Errorf("belongsToStack() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWatcher_EmitEvent(t *testing.T) {
	watcher := NewWatcher(nil, "teststack")

	event := Event{
		Type:        EventTypeCreated,
		TaskID:      "task123",
		ServiceName: "teststack_web",
		Timestamp:   time.Now(),
		Message:     "Test event",
	}

	// Emit event
	watcher.emitEvent(event)

	// Verify event is in channel
	select {
	case receivedEvent := <-watcher.eventChan:
		if receivedEvent.TaskID != event.TaskID {
			t.Errorf("Expected task ID '%s', got '%s'", event.TaskID, receivedEvent.TaskID)
		}
		if receivedEvent.Type != event.Type {
			t.Errorf("Expected event type %s, got %s", event.Type, receivedEvent.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for event")
	}
}

func TestWatcher_GetTaskState(t *testing.T) {
	watcher := NewWatcher(nil, "teststack")

	// No state initially
	_, _, exists := watcher.GetTaskState("task123")
	if exists {
		t.Error("Expected task state to not exist initially")
	}

	// Add task state
	watcher.taskStates["task123"] = &taskState{
		taskID:       "task123",
		state:        "running",
		desiredState: "running",
		lastSeen:     time.Now(),
	}

	// Get task state
	state, desiredState, exists := watcher.GetTaskState("task123")
	if !exists {
		t.Error("Expected task state to exist")
	}
	if state != "running" {
		t.Errorf("Expected state 'running', got '%s'", state)
	}
	if desiredState != "running" {
		t.Errorf("Expected desired state 'running', got '%s'", desiredState)
	}
}

func TestWatcher_GetTrackedTasks(t *testing.T) {
	watcher := NewWatcher(nil, "teststack")

	// No tasks initially
	tasks := watcher.GetTrackedTasks()
	if len(tasks) != 0 {
		t.Errorf("Expected 0 tracked tasks, got %d", len(tasks))
	}

	// Add tasks
	watcher.taskStates["task1"] = &taskState{taskID: "task1"}
	watcher.taskStates["task2"] = &taskState{taskID: "task2"}
	watcher.taskStates["task3"] = &taskState{taskID: "task3"}

	tasks = watcher.GetTrackedTasks()
	if len(tasks) != 3 {
		t.Errorf("Expected 3 tracked tasks, got %d", len(tasks))
	}

	// Verify all task IDs are present
	taskMap := make(map[string]bool)
	for _, taskID := range tasks {
		taskMap[taskID] = true
	}

	if !taskMap["task1"] || !taskMap["task2"] || !taskMap["task3"] {
		t.Error("Not all task IDs are present in tracked tasks")
	}
}

func TestWatcher_CleanupOldTasks(t *testing.T) {
	watcher := NewWatcher(nil, "teststack")

	now := time.Now()

	// Add tasks with different last seen times
	watcher.taskStates["old_task"] = &taskState{
		taskID:   "old_task",
		lastSeen: now.Add(-2 * time.Hour),
	}
	watcher.taskStates["recent_task"] = &taskState{
		taskID:   "recent_task",
		lastSeen: now.Add(-10 * time.Minute),
	}
	watcher.taskStates["new_task"] = &taskState{
		taskID:   "new_task",
		lastSeen: now,
	}

	// Cleanup tasks older than 1 hour
	removed := watcher.CleanupOldTasks(1 * time.Hour)

	if removed != 1 {
		t.Errorf("Expected 1 task to be removed, got %d", removed)
	}

	// Verify old task is removed
	_, _, exists := watcher.GetTaskState("old_task")
	if exists {
		t.Error("Expected old_task to be removed")
	}

	// Verify recent tasks still exist
	_, _, exists = watcher.GetTaskState("recent_task")
	if !exists {
		t.Error("Expected recent_task to still exist")
	}

	_, _, exists = watcher.GetTaskState("new_task")
	if !exists {
		t.Error("Expected new_task to still exist")
	}
}

func TestWatcher_HandleTaskEvent(t *testing.T) {
	watcher := NewWatcher(nil, "mystack")
	ctx := context.Background()

	dockerEvent := events.Message{
		Type:   "task",
		Action: "create",
		Actor: events.Actor{
			ID: "task123",
			Attributes: map[string]string{
				"com.docker.swarm.service.id":   "service456",
				"com.docker.swarm.service.name": "mystack_web",
				"com.docker.swarm.node.id":      "node001",
				"container":                     "container789",
				"desired-state":                 "running",
			},
		},
		Time: time.Now().Unix(),
	}

	// Handle event
	watcher.handleTaskEvent(ctx, dockerEvent)

	// Verify task state is created
	_, _, exists := watcher.GetTaskState("task123")
	if !exists {
		t.Error("Expected task state to be created")
	}

	// Verify event is emitted
	select {
	case event := <-watcher.eventChan:
		if event.Type != EventTypeCreated {
			t.Errorf("Expected event type %s, got %s", EventTypeCreated, event.Type)
		}
		if event.TaskID != "task123" {
			t.Errorf("Expected task ID 'task123', got '%s'", event.TaskID)
		}
		if event.ServiceName != "mystack_web" {
			t.Errorf("Expected service name 'mystack_web', got '%s'", event.ServiceName)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for event")
	}
}

func TestWatcher_HandleContainerHealthEvent(t *testing.T) {
	watcher := NewWatcher(nil, "mystack")
	ctx := context.Background()

	// Add task state first
	watcher.taskStates["task123"] = &taskState{
		taskID:      "task123",
		serviceID:   "service456",
		serviceName: "mystack_web",
	}

	dockerEvent := events.Message{
		Type:   "container",
		Action: "health_status: healthy",
		Actor: events.Actor{
			ID: "container789",
			Attributes: map[string]string{
				"com.docker.swarm.task.id":      "task123",
				"com.docker.swarm.service.id":   "service456",
				"com.docker.swarm.service.name": "mystack_web",
				"com.docker.swarm.node.id":      "node001",
			},
		},
		Time: time.Now().Unix(),
	}

	// Handle event
	watcher.handleContainerEvent(ctx, dockerEvent)

	// Verify event is emitted
	select {
	case event := <-watcher.eventChan:
		if event.Type != EventTypeHealthy {
			t.Errorf("Expected event type %s, got %s", EventTypeHealthy, event.Type)
		}
		if event.TaskID != "task123" {
			t.Errorf("Expected task ID 'task123', got '%s'", event.TaskID)
		}
		if !strings.Contains(event.Message, "healthy") {
			t.Errorf("Expected message to contain 'healthy', got '%s'", event.Message)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for health event")
	}
}
