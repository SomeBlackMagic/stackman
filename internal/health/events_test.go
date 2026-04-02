package health_test

import (
	"testing"

	healthmod "github.com/SomeBlackMagic/stackman/internal/health"
)

func TestEventTypes_ConstantsUnique_PositiveCase(t *testing.T) {
	t.Parallel()

	types := []healthmod.EventType{
		healthmod.EventTypeCreated,
		healthmod.EventTypeStarted,
		healthmod.EventTypeRunning,
		healthmod.EventTypeHealthy,
		healthmod.EventTypeUnhealthy,
		healthmod.EventTypeFailed,
		healthmod.EventTypeShutdown,
	}

	seen := make(map[healthmod.EventType]bool, len(types))
	for _, eventType := range types {
		if eventType == "" {
			t.Fatal("event type should not be empty")
		}
		if seen[eventType] {
			t.Fatalf("duplicate event type: %q", eventType)
		}
		seen[eventType] = true
	}
}

func TestEvent_IsTerminal_EdgeCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		eventType healthmod.EventType
		want      bool
	}{
		{name: "healthy is terminal", eventType: healthmod.EventTypeHealthy, want: true},
		{name: "failed is terminal", eventType: healthmod.EventTypeFailed, want: true},
		{name: "shutdown is terminal", eventType: healthmod.EventTypeShutdown, want: true},
		{name: "running is not terminal", eventType: healthmod.EventTypeRunning, want: false},
		{name: "started is not terminal", eventType: healthmod.EventTypeStarted, want: false},
		{name: "unhealthy is not terminal", eventType: healthmod.EventTypeUnhealthy, want: false},
		{name: "unknown is not terminal", eventType: healthmod.EventType("unknown"), want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			event := healthmod.Event{Type: tc.eventType}
			if got := event.IsTerminal(); got != tc.want {
				t.Fatalf("IsTerminal()=%v, want %v for event type %q", got, tc.want, tc.eventType)
			}
		})
	}
}

func TestEvent_IsFailure_EdgeCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		eventType healthmod.EventType
		want      bool
	}{
		{name: "failed is failure", eventType: healthmod.EventTypeFailed, want: true},
		{name: "unhealthy is failure", eventType: healthmod.EventTypeUnhealthy, want: true},
		{name: "healthy is not failure", eventType: healthmod.EventTypeHealthy, want: false},
		{name: "created is not failure", eventType: healthmod.EventTypeCreated, want: false},
		{name: "unknown is not failure", eventType: healthmod.EventType("unknown"), want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			event := healthmod.Event{Type: tc.eventType}
			if got := event.IsFailure(); got != tc.want {
				t.Fatalf("IsFailure()=%v, want %v for event type %q", got, tc.want, tc.eventType)
			}
		})
	}
}
