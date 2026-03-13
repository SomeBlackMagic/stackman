package health

import (
	"context"
	"testing"
	"time"
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

	if monitor.SendEvent(event) {
		t.Error("SendEvent should fail after stop")
	}
}

func TestMonitor_HandleEvent(t *testing.T) {
	monitor := NewMonitor(nil, "task123", "service456", "mystack_web")

	testCases := []struct {
		name             string
		event            Event
		wantContainerID  string
		wantState        string
		wantHealthStatus string
		wantHealthChecks int
		wantFailedChecks int
	}{
		{
			name: "Created",
			event: Event{
				Type:         EventTypeCreated,
				TaskID:       "task123",
				ContainerID:  "container789",
				State:        "created",
				DesiredState: "running",
			},
			wantContainerID: "container789",
			wantState:       "created",
		},
		{
			name: "Healthy",
			event: Event{
				Type:   EventTypeHealthy,
				TaskID: "task123",
			},
			wantHealthStatus: "healthy",
			wantHealthChecks: 1,
		},
		{
			name: "Unhealthy",
			event: Event{
				Type:   EventTypeUnhealthy,
				TaskID: "task123",
			},
			wantHealthStatus: "unhealthy",
			wantHealthChecks: 2,
			wantFailedChecks: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			monitor.handleEvent(tc.event)

			if tc.wantContainerID != "" && monitor.containerID != tc.wantContainerID {
				t.Errorf("Expected container ID %q, got %q", tc.wantContainerID, monitor.containerID)
			}
			if tc.wantState != "" && monitor.state != tc.wantState {
				t.Errorf("Expected state %q, got %q", tc.wantState, monitor.state)
			}
			if tc.wantHealthStatus != "" && monitor.healthStatus != tc.wantHealthStatus {
				t.Errorf("Expected health status %q, got %q", tc.wantHealthStatus, monitor.healthStatus)
			}
			if tc.wantHealthChecks > 0 && monitor.healthChecks != tc.wantHealthChecks {
				t.Errorf("Expected %d health checks, got %d", tc.wantHealthChecks, monitor.healthChecks)
			}
			if tc.wantFailedChecks > 0 && monitor.failedChecks != tc.wantFailedChecks {
				t.Errorf("Expected %d failed checks, got %d", tc.wantFailedChecks, monitor.failedChecks)
			}
		})
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
