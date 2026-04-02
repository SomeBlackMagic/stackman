package health_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	dockerswarm "github.com/docker/docker/api/types/swarm"

	healthmod "github.com/SomeBlackMagic/stackman/internal/health"
	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestServiceUpdateMonitor_WaitHealthy_PositiveCaseAllRunning(t *testing.T) {
	t.Parallel()

	replicas := uint64(2)
	mock := &swarmint.MockDockerClient{
		TaskListFn: func(_ context.Context, _ dockertypes.TaskListOptions) ([]dockerswarm.Task, error) {
			return []dockerswarm.Task{
				{
					ID:           "task1",
					DesiredState: dockerswarm.TaskStateRunning,
					Status: dockerswarm.TaskStatus{
						State: dockerswarm.TaskStateRunning,
					},
				},
				{
					ID:           "task2",
					DesiredState: dockerswarm.TaskStateRunning,
					Status: dockerswarm.TaskStatus{
						State: dockerswarm.TaskStateRunning,
					},
				},
			}, nil
		},
		ServiceInspectWithRawFn: func(_ context.Context, _ string, _ dockertypes.ServiceInspectOptions) (dockerswarm.Service, []byte, error) {
			return dockerswarm.Service{
				Spec: dockerswarm.ServiceSpec{
					Mode: dockerswarm.ServiceMode{
						Replicated: &dockerswarm.ReplicatedService{Replicas: &replicas},
					},
				},
			}, nil, nil
		},
	}

	monitor := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := monitor.WaitHealthy(ctx, 2*time.Second); err != nil {
		t.Fatalf("expected healthy service, got error: %v", err)
	}
}

func TestServiceUpdateMonitor_WaitHealthy_NegativeCaseTimeout(t *testing.T) {
	t.Parallel()

	replicas := uint64(2)
	mock := &swarmint.MockDockerClient{
		TaskListFn: func(_ context.Context, _ dockertypes.TaskListOptions) ([]dockerswarm.Task, error) {
			return []dockerswarm.Task{
				{
					ID:           "task1",
					DesiredState: dockerswarm.TaskStateRunning,
					Status: dockerswarm.TaskStatus{
						State: dockerswarm.TaskStateRunning,
					},
				},
			}, nil
		},
		ServiceInspectWithRawFn: func(_ context.Context, _ string, _ dockertypes.ServiceInspectOptions) (dockerswarm.Service, []byte, error) {
			return dockerswarm.Service{
				Spec: dockerswarm.ServiceSpec{
					Mode: dockerswarm.ServiceMode{
						Replicated: &dockerswarm.ReplicatedService{Replicas: &replicas},
					},
				},
			}, nil, nil
		},
	}

	monitor := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
	err := monitor.WaitHealthy(context.Background(), 120*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestServiceUpdateMonitor_WaitHealthy_NegativeCaseFailedTask(t *testing.T) {
	t.Parallel()

	replicas := uint64(1)
	mock := &swarmint.MockDockerClient{
		TaskListFn: func(_ context.Context, _ dockertypes.TaskListOptions) ([]dockerswarm.Task, error) {
			return []dockerswarm.Task{
				{
					ID: "task-failed",
					Status: dockerswarm.TaskStatus{
						State: dockerswarm.TaskStateFailed,
						Err:   "container exited with code 1",
					},
				},
			}, nil
		},
		ServiceInspectWithRawFn: func(_ context.Context, _ string, _ dockertypes.ServiceInspectOptions) (dockerswarm.Service, []byte, error) {
			return dockerswarm.Service{
				Spec: dockerswarm.ServiceSpec{
					Mode: dockerswarm.ServiceMode{
						Replicated: &dockerswarm.ReplicatedService{Replicas: &replicas},
					},
				},
			}, nil, nil
		},
	}

	monitor := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
	err := monitor.WaitHealthy(context.Background(), 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected failed task error")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected failed task message, got: %v", err)
	}
}

func TestServiceUpdateMonitor_WaitHealthy_NegativeCaseTaskListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("task list failed")
	mock := &swarmint.MockDockerClient{
		TaskListFn: func(_ context.Context, _ dockertypes.TaskListOptions) ([]dockerswarm.Task, error) {
			return nil, wantErr
		},
		ServiceInspectWithRawFn: func(_ context.Context, _ string, _ dockertypes.ServiceInspectOptions) (dockerswarm.Service, []byte, error) {
			return dockerswarm.Service{}, nil, nil
		},
	}

	monitor := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
	err := monitor.WaitHealthy(context.Background(), 500*time.Millisecond)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped error %v, got %v", wantErr, err)
	}
}

func TestServiceUpdateMonitor_WaitHealthy_NegativeCaseServiceInspectError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("inspect failed")
	mock := &swarmint.MockDockerClient{
		TaskListFn: func(_ context.Context, _ dockertypes.TaskListOptions) ([]dockerswarm.Task, error) {
			return []dockerswarm.Task{
				{
					ID: "task1",
					Status: dockerswarm.TaskStatus{
						State: dockerswarm.TaskStateRunning,
					},
				},
			}, nil
		},
		ServiceInspectWithRawFn: func(_ context.Context, _ string, _ dockertypes.ServiceInspectOptions) (dockerswarm.Service, []byte, error) {
			return dockerswarm.Service{}, nil, wantErr
		},
	}

	monitor := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
	err := monitor.WaitHealthy(context.Background(), 500*time.Millisecond)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped error %v, got %v", wantErr, err)
	}
}

func TestServiceUpdateMonitor_WaitHealthy_NegativeCaseRejectedTaskWithoutError(t *testing.T) {
	t.Parallel()

	replicas := uint64(1)
	mock := &swarmint.MockDockerClient{
		TaskListFn: func(_ context.Context, _ dockertypes.TaskListOptions) ([]dockerswarm.Task, error) {
			return []dockerswarm.Task{
				{
					ID: "task-rejected",
					Status: dockerswarm.TaskStatus{
						State: dockerswarm.TaskStateRejected,
					},
				},
			}, nil
		},
		ServiceInspectWithRawFn: func(_ context.Context, _ string, _ dockertypes.ServiceInspectOptions) (dockerswarm.Service, []byte, error) {
			return dockerswarm.Service{
				Spec: dockerswarm.ServiceSpec{
					Mode: dockerswarm.ServiceMode{
						Replicated: &dockerswarm.ReplicatedService{Replicas: &replicas},
					},
				},
			}, nil, nil
		},
	}

	monitor := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
	err := monitor.WaitHealthy(context.Background(), 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected rejected task error")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected failed task message, got: %v", err)
	}
}

func TestServiceUpdateMonitor_WaitHealthy_PositiveCaseZeroReplicas(t *testing.T) {
	t.Parallel()

	zero := uint64(0)
	mock := &swarmint.MockDockerClient{
		TaskListFn: func(_ context.Context, _ dockertypes.TaskListOptions) ([]dockerswarm.Task, error) {
			return nil, nil
		},
		ServiceInspectWithRawFn: func(_ context.Context, _ string, _ dockertypes.ServiceInspectOptions) (dockerswarm.Service, []byte, error) {
			return dockerswarm.Service{
				Spec: dockerswarm.ServiceSpec{
					Mode: dockerswarm.ServiceMode{
						Replicated: &dockerswarm.ReplicatedService{Replicas: &zero},
					},
				},
			}, nil, nil
		},
	}

	monitor := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
	if err := monitor.WaitHealthy(context.Background(), 200*time.Millisecond); err != nil {
		t.Fatalf("expected healthy state for zero replicas, got: %v", err)
	}
}

func TestServiceUpdateMonitor_WaitHealthy_PositiveCaseGlobalService(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		TaskListFn: func(_ context.Context, _ dockertypes.TaskListOptions) ([]dockerswarm.Task, error) {
			return []dockerswarm.Task{
				{
					ID: "task-global-1",
					Status: dockerswarm.TaskStatus{
						State: dockerswarm.TaskStateRunning,
					},
				},
			}, nil
		},
		ServiceInspectWithRawFn: func(_ context.Context, _ string, _ dockertypes.ServiceInspectOptions) (dockerswarm.Service, []byte, error) {
			return dockerswarm.Service{
				Spec: dockerswarm.ServiceSpec{
					Mode: dockerswarm.ServiceMode{
						Global: &dockerswarm.GlobalService{},
					},
				},
			}, nil, nil
		},
	}

	monitor := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
	if err := monitor.WaitHealthy(context.Background(), 500*time.Millisecond); err != nil {
		t.Fatalf("expected healthy global service, got error: %v", err)
	}
}

func TestServiceUpdateMonitor_WaitHealthy_NegativeCaseCanceledContext(t *testing.T) {
	t.Parallel()

	replicas := uint64(2)
	mock := &swarmint.MockDockerClient{
		TaskListFn: func(_ context.Context, _ dockertypes.TaskListOptions) ([]dockerswarm.Task, error) {
			return []dockerswarm.Task{
				{
					ID: "task1",
					Status: dockerswarm.TaskStatus{
						State: dockerswarm.TaskStateRunning,
					},
				},
			}, nil
		},
		ServiceInspectWithRawFn: func(_ context.Context, _ string, _ dockertypes.ServiceInspectOptions) (dockerswarm.Service, []byte, error) {
			return dockerswarm.Service{
				Spec: dockerswarm.ServiceSpec{
					Mode: dockerswarm.ServiceMode{
						Replicated: &dockerswarm.ReplicatedService{Replicas: &replicas},
					},
				},
			}, nil, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	monitor := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
	err := monitor.WaitHealthy(ctx, 5*time.Second)
	if err == nil {
		t.Fatal("expected canceled context error")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected canceled message, got: %v", err)
	}
}
