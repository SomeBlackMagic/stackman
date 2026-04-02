package swarm_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestRemoveExitedContainers_PositiveCase(t *testing.T) {
	t.Parallel()

	removed := make(map[string]bool)
	mock := &swarmint.MockDockerClient{
		ContainerListFn: func(_ context.Context, opts container.ListOptions) ([]dockertypes.Container, error) {
			if !opts.All {
				t.Fatal("expected ContainerList All=true")
			}

			labels := opts.Filters.Get("label")
			if len(labels) != 1 || labels[0] != "com.docker.stack.namespace=mystack" {
				t.Fatalf("unexpected label filters: %v", labels)
			}

			statuses := opts.Filters.Get("status")
			if len(statuses) != 2 {
				t.Fatalf("expected 2 status filters, got %v", statuses)
			}
			if !containsString(statuses, "exited") || !containsString(statuses, "dead") {
				t.Fatalf("expected status filters exited/dead, got %v", statuses)
			}

			return []dockertypes.Container{
				{ID: "c1", Status: "exited"},
				{ID: "c2", Status: "dead"},
			}, nil
		},
		ContainerRemoveFn: func(_ context.Context, id string, opts container.RemoveOptions) error {
			if !opts.Force {
				t.Fatal("expected Force=true for ContainerRemove")
			}
			if opts.RemoveVolumes {
				t.Fatal("expected RemoveVolumes=false for ContainerRemove")
			}
			removed[id] = true
			return nil
		},
	}

	err := swarmint.RemoveExitedContainers(context.Background(), mock, "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["c1"] || !removed["c2"] {
		t.Fatalf("expected both exited containers to be removed, removed=%v", removed)
	}
}

func TestRemoveExitedContainers_EdgeCase(t *testing.T) {
	t.Parallel()

	removeCalled := 0
	mock := &swarmint.MockDockerClient{
		ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]dockertypes.Container, error) {
			return nil, nil
		},
		ContainerRemoveFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
			removeCalled++
			return nil
		},
	}

	err := swarmint.RemoveExitedContainers(context.Background(), mock, "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removeCalled != 0 {
		t.Fatalf("expected no ContainerRemove calls, got %d", removeCalled)
	}
}

func TestRemoveExitedContainers_NegativeCaseListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("list failed")
	mock := &swarmint.MockDockerClient{
		ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]dockertypes.Container, error) {
			return nil, wantErr
		},
	}

	err := swarmint.RemoveExitedContainers(context.Background(), mock, "mystack")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped list error, got %v", err)
	}
	if !strings.Contains(err.Error(), "list exited containers") {
		t.Fatalf("expected list context in error, got %v", err)
	}
}

func TestRemoveExitedContainers_NegativeCaseRemoveError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("remove failed")
	mock := &swarmint.MockDockerClient{
		ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]dockertypes.Container, error) {
			return []dockertypes.Container{{ID: "c1", Status: "exited"}}, nil
		},
		ContainerRemoveFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
			return wantErr
		},
	}

	err := swarmint.RemoveExitedContainers(context.Background(), mock, "mystack")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped remove error, got %v", err)
	}
	if !strings.Contains(err.Error(), "remove container") {
		t.Fatalf("expected remove context in error, got %v", err)
	}
}

func TestPruneOrphanedServices_PositiveCase(t *testing.T) {
	t.Parallel()

	removed := make(map[string]bool)
	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, opts dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			labels := opts.Filters.Get("label")
			if len(labels) != 1 || labels[0] != "com.docker.stack.namespace=mystack" {
				t.Fatalf("unexpected label filters: %v", labels)
			}
			return []swarm.Service{
				{
					ID:   "svc1",
					Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_web"}},
				},
				{
					ID:   "svc2",
					Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_legacy"}},
				},
			}, nil
		},
		ServiceRemoveFn: func(_ context.Context, id string) error {
			removed[id] = true
			return nil
		},
	}

	desired := map[string]bool{"web": true}
	err := swarmint.PruneOrphanedServices(context.Background(), mock, "mystack", desired)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed["svc1"] {
		t.Fatal("service mystack_web should not be removed")
	}
	if !removed["svc2"] {
		t.Fatal("service mystack_legacy should be removed")
	}
}

func TestPruneOrphanedServices_EdgeCase(t *testing.T) {
	t.Parallel()

	removeCalled := 0
	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, nil
		},
		ServiceRemoveFn: func(_ context.Context, _ string) error {
			removeCalled++
			return nil
		},
	}

	err := swarmint.PruneOrphanedServices(context.Background(), mock, "mystack", map[string]bool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removeCalled != 0 {
		t.Fatalf("expected no ServiceRemove calls, got %d", removeCalled)
	}
}

func TestPruneOrphanedServices_NegativeCaseListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("service list failed")
	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, wantErr
		},
	}

	err := swarmint.PruneOrphanedServices(context.Background(), mock, "mystack", map[string]bool{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped list error, got %v", err)
	}
	if !strings.Contains(err.Error(), "list services") {
		t.Fatalf("expected list context in error, got %v", err)
	}
}

func TestPruneOrphanedServices_NegativeCaseRemoveError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("service remove failed")
	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return []swarm.Service{
				{
					ID:   "svc2",
					Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_legacy"}},
				},
			}, nil
		},
		ServiceRemoveFn: func(_ context.Context, _ string) error {
			return wantErr
		},
	}

	err := swarmint.PruneOrphanedServices(context.Background(), mock, "mystack", map[string]bool{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped remove error, got %v", err)
	}
	if !strings.Contains(err.Error(), "remove orphaned service") {
		t.Fatalf("expected remove context in error, got %v", err)
	}
}

func TestPruneOrphanedNetworks_PositiveCase(t *testing.T) {
	t.Parallel()

	removed := make(map[string]bool)
	mock := &swarmint.MockDockerClient{
		NetworkListFn: func(_ context.Context, opts network.ListOptions) ([]network.Summary, error) {
			labels := opts.Filters.Get("label")
			if len(labels) != 1 || labels[0] != "com.docker.stack.namespace=mystack" {
				t.Fatalf("unexpected label filters: %v", labels)
			}
			return []network.Summary{
				{ID: "net1", Name: "mystack_frontend"},
				{ID: "net2", Name: "mystack_legacy-net"},
			}, nil
		},
		NetworkRemoveFn: func(_ context.Context, id string) error {
			removed[id] = true
			return nil
		},
	}

	desired := map[string]bool{"frontend": true}
	err := swarmint.PruneOrphanedNetworks(context.Background(), mock, "mystack", desired)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed["net1"] {
		t.Fatal("network mystack_frontend should not be removed")
	}
	if !removed["net2"] {
		t.Fatal("network mystack_legacy-net should be removed")
	}
}

func TestPruneOrphanedNetworks_EdgeCase(t *testing.T) {
	t.Parallel()

	removeCalled := 0
	mock := &swarmint.MockDockerClient{
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		NetworkRemoveFn: func(_ context.Context, _ string) error {
			removeCalled++
			return nil
		},
	}

	err := swarmint.PruneOrphanedNetworks(context.Background(), mock, "mystack", map[string]bool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removeCalled != 0 {
		t.Fatalf("expected no NetworkRemove calls, got %d", removeCalled)
	}
}

func TestPruneOrphanedNetworks_NegativeCaseListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("network list failed")
	mock := &swarmint.MockDockerClient{
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, wantErr
		},
	}

	err := swarmint.PruneOrphanedNetworks(context.Background(), mock, "mystack", map[string]bool{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped list error, got %v", err)
	}
	if !strings.Contains(err.Error(), "list networks") {
		t.Fatalf("expected list context in error, got %v", err)
	}
}

func TestPruneOrphanedNetworks_NegativeCaseRemoveError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("network remove failed")
	mock := &swarmint.MockDockerClient{
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return []network.Summary{{ID: "net2", Name: "mystack_legacy-net"}}, nil
		},
		NetworkRemoveFn: func(_ context.Context, _ string) error {
			return wantErr
		},
	}

	err := swarmint.PruneOrphanedNetworks(context.Background(), mock, "mystack", map[string]bool{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped remove error, got %v", err)
	}
	if !strings.Contains(err.Error(), "remove orphaned network") {
		t.Fatalf("expected remove context in error, got %v", err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
