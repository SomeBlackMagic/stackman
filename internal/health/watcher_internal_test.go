package health

import (
	"testing"
	"time"

	dockerevents "github.com/docker/docker/api/types/events"
)

func TestWatcher_convertEvent_PositiveCaseMappings(t *testing.T) {
	t.Parallel()

	watcher := NewWatcher(nil, "mystack")
	tests := []struct {
		name   string
		action dockerevents.Action
		want   EventType
	}{
		{name: "create", action: "create", want: EventTypeCreated},
		{name: "start", action: "start", want: EventTypeStarted},
		{name: "die", action: "die", want: EventTypeFailed},
		{name: "kill", action: "kill", want: EventTypeFailed},
		{name: "destroy", action: "destroy", want: EventTypeShutdown},
		{name: "healthy", action: "health_status: healthy", want: EventTypeHealthy},
		{name: "unhealthy", action: "health_status: unhealthy", want: EventTypeUnhealthy},
		{name: "health-status-other", action: "health_status: starting", want: EventTypeUnhealthy},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := dockerevents.Message{
				Action: tc.action,
				Actor: dockerevents.Actor{
					ID: "container1",
					Attributes: map[string]string{
						"com.docker.swarm.task.id":    "task1",
						"com.docker.swarm.service.id": "service1",
					},
				},
				Time: 123,
			}

			event := watcher.convertEvent(msg)
			if event == nil {
				t.Fatalf("expected event for action %q", tc.action)
			}
			if event.Type != tc.want {
				t.Fatalf("event type mismatch: got %q, want %q", event.Type, tc.want)
			}
			if event.TaskID != "task1" || event.ServiceID != "service1" || event.ContainerID != "container1" {
				t.Fatalf("unexpected IDs in event: %+v", *event)
			}
			if event.Message != string(tc.action) {
				t.Fatalf("unexpected message: got %q, want %q", event.Message, tc.action)
			}
			if event.Timestamp.IsZero() {
				t.Fatal("expected non-zero timestamp")
			}
		})
	}
}

func TestWatcher_convertEvent_NegativeCaseUnknownAction(t *testing.T) {
	t.Parallel()

	watcher := NewWatcher(nil, "mystack")
	event := watcher.convertEvent(dockerevents.Message{
		Action: "rename",
		Actor: dockerevents.Actor{
			Attributes: map[string]string{
				"com.docker.swarm.task.id": "task1",
			},
		},
	})
	if event != nil {
		t.Fatalf("expected nil for unknown action, got %+v", *event)
	}
}

func TestEventTimestamp_EdgeCaseTimePriority(t *testing.T) {
	t.Parallel()

	fromNano := eventTimestamp(dockerevents.Message{Time: 10, TimeNano: 1234567890})
	if !fromNano.Equal(time.Unix(0, 1234567890).UTC()) {
		t.Fatalf("expected nano timestamp, got %s", fromNano)
	}

	fromSeconds := eventTimestamp(dockerevents.Message{Time: 1700000000})
	if !fromSeconds.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Fatalf("expected second timestamp, got %s", fromSeconds)
	}

	fromNow := eventTimestamp(dockerevents.Message{})
	if fromNow.IsZero() {
		t.Fatal("expected non-zero fallback timestamp")
	}
}
