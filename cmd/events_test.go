package cmd

import (
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"
)

func TestEventsOptions(t *testing.T) {
	// Test that EventsOptions struct is properly defined
	opts := &EventsOptions{
		ServiceName: "web",
		Since:       "10m",
		Until:       "now",
		Follow:      true,
	}

	if opts.ServiceName != "web" {
		t.Errorf("Expected ServiceName 'web', got '%s'", opts.ServiceName)
	}
	if opts.Since != "10m" {
		t.Errorf("Expected Since '10m', got '%s'", opts.Since)
	}
	if opts.Until != "now" {
		t.Errorf("Expected Until 'now', got '%s'", opts.Until)
	}
	if !opts.Follow {
		t.Error("Expected Follow to be true")
	}
}

func TestDisplayEvent(t *testing.T) {
	// Test displayEvent function with different event types
	serviceNameMap := map[string]string{
		"service123": "web",
	}

	// Test service event
	serviceEvent := events.Message{
		Type:   "service",
		Action: "update",
		Time:   time.Now().Unix(),
		Actor: events.Actor{
			ID: "service123",
			Attributes: map[string]string{
				"name":            "test-stack_web",
				"updatestate.new": "completed",
			},
		},
	}

	// Just ensure displayEvent doesn't panic
	displayEvent(serviceEvent, serviceNameMap, "test-stack")

	// Test task event
	taskEvent := events.Message{
		Type:   "task",
		Action: "start",
		Time:   time.Now().Unix(),
		Actor: events.Actor{
			ID: "task1234567890ab",
			Attributes: map[string]string{
				"com.docker.swarm.service.id":   "service123",
				"com.docker.swarm.service.name": "test-stack_web",
				"desiredstate":                  "running",
				"currentstate":                  "running",
			},
		},
	}

	displayEvent(taskEvent, serviceNameMap, "test-stack")

	// Test container event
	containerEvent := events.Message{
		Type:   "container",
		Action: "start",
		Time:   time.Now().Unix(),
		Actor: events.Actor{
			ID: "container1234567890ab",
			Attributes: map[string]string{
				"com.docker.swarm.task.id": "task1234567890ab",
			},
		},
	}

	displayEvent(containerEvent, serviceNameMap, "test-stack")
}
