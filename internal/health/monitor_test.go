package health_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	dockerevents "github.com/docker/docker/api/types/events"

	healthmod "github.com/SomeBlackMagic/stackman/internal/health"
	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestWatcher_Start_PositiveCaseConvertsDieToFailed(t *testing.T) {
	t.Parallel()

	dockerEvents := make(chan dockerevents.Message, 1)
	dockerErrs := make(chan error, 1)
	mock := &swarmint.MockDockerClient{
		EventsFn: func(_ context.Context, _ dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
			return dockerEvents, dockerErrs
		},
	}

	watcher := healthmod.NewWatcher(mock, "mystack")
	subscriber := watcher.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- watcher.Start(ctx)
	}()

	dockerEvents <- dockerevents.Message{
		Type:   "container",
		Action: "die",
		Actor: dockerevents.Actor{
			Attributes: map[string]string{
				"com.docker.stack.namespace": "mystack",
				"com.docker.swarm.task.id":   "task1",
			},
		},
	}

	select {
	case event := <-subscriber:
		if event.Type != healthmod.EventTypeFailed {
			t.Fatalf("expected %q, got %q", healthmod.EventTypeFailed, event.Type)
		}
		if event.TaskID != "task1" {
			t.Fatalf("expected task1, got %q", event.TaskID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for watcher event")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for watcher shutdown")
	}
}

func TestWatcher_Start_EdgeCaseIgnoresForeignStackEvents(t *testing.T) {
	t.Parallel()

	dockerEvents := make(chan dockerevents.Message, 1)
	dockerErrs := make(chan error, 1)
	mock := &swarmint.MockDockerClient{
		EventsFn: func(_ context.Context, _ dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
			return dockerEvents, dockerErrs
		},
	}

	watcher := healthmod.NewWatcher(mock, "mystack")
	subscriber := watcher.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- watcher.Start(ctx)
	}()

	dockerEvents <- dockerevents.Message{
		Type:   "container",
		Action: "die",
		Actor: dockerevents.Actor{
			Attributes: map[string]string{
				"com.docker.stack.namespace": "otherstack",
				"com.docker.swarm.task.id":   "task1",
			},
		},
	}

	select {
	case event := <-subscriber:
		t.Fatalf("did not expect event for foreign stack, got %+v", event)
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for watcher shutdown")
	}
}

func TestWatcher_Start_NegativeCaseReturnsDockerError(t *testing.T) {
	t.Parallel()

	dockerEvents := make(chan dockerevents.Message)
	dockerErrs := make(chan error, 1)
	wantErr := errors.New("docker event stream broken")
	mock := &swarmint.MockDockerClient{
		EventsFn: func(_ context.Context, _ dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
			return dockerEvents, dockerErrs
		},
	}

	watcher := healthmod.NewWatcher(mock, "mystack")
	dockerErrs <- wantErr

	err := watcher.Start(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped stream error %v, got %v", wantErr, err)
	}
}

func TestWatcher_Start_PositiveCaseStreamClosedReturnsNil(t *testing.T) {
	t.Parallel()

	dockerEvents := make(chan dockerevents.Message)
	dockerErrs := make(chan error)
	close(dockerEvents)
	close(dockerErrs)

	mock := &swarmint.MockDockerClient{
		EventsFn: func(_ context.Context, _ dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
			return dockerEvents, dockerErrs
		},
	}

	watcher := healthmod.NewWatcher(mock, "mystack")
	if err := watcher.Start(context.Background()); err != nil {
		t.Fatalf("expected nil error on closed streams, got %v", err)
	}
}

func TestWatcher_Start_EdgeCaseIgnoresUnknownAction(t *testing.T) {
	t.Parallel()

	dockerEvents := make(chan dockerevents.Message, 1)
	dockerErrs := make(chan error, 1)
	mock := &swarmint.MockDockerClient{
		EventsFn: func(_ context.Context, _ dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
			return dockerEvents, dockerErrs
		},
	}

	watcher := healthmod.NewWatcher(mock, "mystack")
	subscriber := watcher.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- watcher.Start(ctx)
	}()

	dockerEvents <- dockerevents.Message{
		Action: "rename",
		Actor: dockerevents.Actor{
			Attributes: map[string]string{
				"com.docker.stack.namespace": "mystack",
			},
		},
	}

	select {
	case event := <-subscriber:
		t.Fatalf("did not expect event for unknown action, got %+v", event)
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled or nil, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for watcher shutdown")
	}
}

func TestWatcher_Start_EdgeCaseContinuesOnNilError(t *testing.T) {
	t.Parallel()

	dockerEvents := make(chan dockerevents.Message, 1)
	dockerErrs := make(chan error, 1)
	mock := &swarmint.MockDockerClient{
		EventsFn: func(_ context.Context, _ dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
			return dockerEvents, dockerErrs
		},
	}

	watcher := healthmod.NewWatcher(mock, "mystack")
	subscriber := watcher.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- watcher.Start(ctx)
	}()

	dockerErrs <- nil
	dockerEvents <- dockerevents.Message{
		Action: "health_status: healthy",
		Actor: dockerevents.Actor{
			Attributes: map[string]string{
				"com.docker.stack.namespace": "mystack",
			},
		},
	}

	select {
	case event := <-subscriber:
		if event.Type != healthmod.EventTypeHealthy {
			t.Fatalf("expected %q, got %q", healthmod.EventTypeHealthy, event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for healthy event")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled or nil, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for watcher shutdown")
	}
}

func TestWatcher_Start_NegativeCaseWrapsErrorMessage(t *testing.T) {
	t.Parallel()

	dockerEvents := make(chan dockerevents.Message)
	dockerErrs := make(chan error, 1)
	mock := &swarmint.MockDockerClient{
		EventsFn: func(_ context.Context, _ dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
			return dockerEvents, dockerErrs
		},
	}

	watcher := healthmod.NewWatcher(mock, "mystack")
	dockerErrs <- fmt.Errorf("inner stream error")

	err := watcher.Start(context.Background())
	if err == nil {
		t.Fatal("expected stream error")
	}
	if !strings.Contains(err.Error(), "watch docker events") {
		t.Fatalf("expected wrapped message, got: %v", err)
	}
}
