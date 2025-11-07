package task

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/client"
)

func TestNewMonitor(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	if monitor.taskID != "task123" {
		t.Errorf("Expected task ID 'task123', got '%s'", monitor.taskID)
	}

	if monitor.serviceID != "service456" {
		t.Errorf("Expected service ID 'service456', got '%s'", monitor.serviceID)
	}

	if monitor.serviceName != "mystack_web" {
		t.Errorf("Expected service name 'mystack_web', got '%s'", monitor.serviceName)
	}

	if monitor.healthStatus != "unknown" {
		t.Errorf("Expected initial health status 'unknown', got '%s'", monitor.healthStatus)
	}
}

func TestMonitor_SendEvent(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	event := Event{
		Type:        EventTypeCreated,
		TaskID:      "task123",
		ServiceID:   "service456",
		ServiceName: "mystack_web",
		Message:     "Task created",
	}

	// Send event should succeed
	if !monitor.SendEvent(event) {
		t.Error("SendEvent failed unexpectedly")
	}

	// Receive event
	select {
	case received := <-monitor.eventChan:
		if received.Type != EventTypeCreated {
			t.Errorf("Expected event type %s, got %s", EventTypeCreated, received.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for event")
	}

	// After stop, SendEvent should fail
	monitor.Stop()
	time.Sleep(10 * time.Millisecond)

	if monitor.SendEvent(event) {
		t.Error("SendEvent should fail after stop")
	}
}

func TestMonitor_HandleEvent(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	// Test Created event
	createdEvent := Event{
		Type:         EventTypeCreated,
		TaskID:       "task123",
		ContainerID:  "container789",
		State:        "created",
		DesiredState: "running",
	}
	monitor.handleEvent(createdEvent)

	if monitor.containerID != "container789" {
		t.Errorf("Expected container ID 'container789', got '%s'", monitor.containerID)
	}

	if monitor.state != "created" {
		t.Errorf("Expected state 'created', got '%s'", monitor.state)
	}

	// Test Healthy event
	healthyEvent := Event{
		Type:   EventTypeHealthy,
		TaskID: "task123",
	}
	monitor.handleEvent(healthyEvent)

	if monitor.healthStatus != "healthy" {
		t.Errorf("Expected health status 'healthy', got '%s'", monitor.healthStatus)
	}

	if monitor.healthChecks != 1 {
		t.Errorf("Expected 1 health check, got %d", monitor.healthChecks)
	}

	// Test Unhealthy event
	unhealthyEvent := Event{
		Type:   EventTypeUnhealthy,
		TaskID: "task123",
	}
	monitor.handleEvent(unhealthyEvent)

	if monitor.healthStatus != "unhealthy" {
		t.Errorf("Expected health status 'unhealthy', got '%s'", monitor.healthStatus)
	}

	if monitor.failedChecks != 1 {
		t.Errorf("Expected 1 failed check, got %d", monitor.failedChecks)
	}

	if monitor.healthChecks != 2 {
		t.Errorf("Expected 2 health checks, got %d", monitor.healthChecks)
	}
}

func TestMonitor_GetState(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	// Set some state
	monitor.state = "running"
	monitor.desiredState = "running"
	monitor.healthStatus = "healthy"

	// Get state should be thread-safe
	state, desired, health := monitor.GetState()

	if state != "running" {
		t.Errorf("Expected state 'running', got '%s'", state)
	}

	if desired != "running" {
		t.Errorf("Expected desired state 'running', got '%s'", desired)
	}

	if health != "healthy" {
		t.Errorf("Expected health 'healthy', got '%s'", health)
	}
}

func TestMonitor_GetStats(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	// Simulate some events
	monitor.healthChecks = 5
	monitor.failedChecks = 2

	checks, failed, lastSeen := monitor.GetStats()

	if checks != 5 {
		t.Errorf("Expected 5 health checks, got %d", checks)
	}

	if failed != 2 {
		t.Errorf("Expected 2 failed checks, got %d", failed)
	}

	if time.Since(lastSeen) > 1*time.Second {
		t.Error("lastSeen should be recent")
	}
}

func TestMonitor_IsHealthy(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	// Initially not healthy
	if monitor.IsHealthy() {
		t.Error("Monitor should not be healthy initially")
	}

	// Set to healthy
	monitor.healthStatus = "healthy"
	if !monitor.IsHealthy() {
		t.Error("Monitor should be healthy")
	}

	// Set to unhealthy
	monitor.healthStatus = "unhealthy"
	if monitor.IsHealthy() {
		t.Error("Monitor should not be healthy")
	}
}

func TestMonitor_Stop(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	// Stop should close channels
	monitor.Stop()

	select {
	case <-monitor.stopChan:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("stopChan should be closed")
	}

	// Context should be cancelled
	select {
	case <-monitor.ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("context should be cancelled")
	}

	// Multiple calls to Stop should be safe
	monitor.Stop()
	monitor.Stop()
}

func TestMonitor_Done(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	// Start monitor in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go monitor.Start(ctx)

	// Stop and wait for done
	monitor.Stop()

	select {
	case <-monitor.Done():
		// Expected
	case <-time.After(1 * time.Second):
		t.Error("Done channel should be closed after stop")
	}
}

func TestMonitor_ShortTaskID(t *testing.T) {
	tests := []struct {
		name     string
		taskID   string
		expected string
	}{
		{"Long ID", "abc123def456ghi789", "abc123def456"},
		{"Short ID", "abc123", "abc123"},
		{"12 char ID", "abc123def456", "abc123def456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := NewMonitor(nil, tt.taskID, "service", "name")
			got := monitor.shortTaskID()
			if got != tt.expected {
				t.Errorf("shortTaskID() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestMonitor_TerminalEventStopsMonitor(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start monitor in background
	go monitor.Start(ctx)

	// Send terminal event
	monitor.SendEvent(Event{
		Type:    EventTypeFailed,
		TaskID:  "task123",
		Message: "Task failed",
	})

	// Monitor should stop automatically
	select {
	case <-monitor.Done():
		// Expected - monitor stopped due to terminal event
	case <-time.After(1 * time.Second):
		t.Error("Monitor should stop after receiving terminal event")
	}
}

func TestMonitor_WaitForHealthy_Success(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	// Start monitor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go monitor.Start(ctx)

	// Set healthy in background after delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		monitor.mu.Lock()
		monitor.healthStatus = "healthy"
		monitor.mu.Unlock()
	}()

	// Wait for healthy
	err := monitor.WaitForHealthy(1 * time.Second)
	if err != nil {
		t.Errorf("WaitForHealthy failed: %v", err)
	}

	monitor.Stop()
}

func TestMonitor_WaitForHealthy_Timeout(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	// Start monitor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go monitor.Start(ctx)

	// Don't set healthy - should timeout
	err := monitor.WaitForHealthy(200 * time.Millisecond)
	if err == nil {
		t.Error("WaitForHealthy should timeout")
	}

	monitor.Stop()
}
