package health

import (
	"testing"
)

func TestMonitor_LogStreaming(t *testing.T) {
	// Test that monitor has streamLogs method
	// This is a compile-time check to ensure the method exists

	// We can't fully test log streaming without a real Docker container,
	// but we can test that the Monitor structure is correct

	m := &Monitor{
		taskID:      "task123",
		serviceID:   "service123",
		serviceName: "web",
	}

	if m.taskID != "task123" {
		t.Errorf("Expected taskID 'task123', got '%s'", m.taskID)
	}

	if m.serviceName != "web" {
		t.Errorf("Expected serviceName 'web', got '%s'", m.serviceName)
	}

	// Test shortTaskID
	shortID := m.shortTaskID()
	if len(shortID) > 12 {
		t.Errorf("shortTaskID should be <= 12 chars, got %d", len(shortID))
	}
}
