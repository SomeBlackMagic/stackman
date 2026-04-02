package swarm_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestGetStackState_EmptyStack(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, nil
		},
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
	}

	state, err := swarmint.GetStackState(context.Background(), mock, "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(state.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(state.Services))
	}
	if len(state.Networks) != 0 {
		t.Errorf("expected 0 networks, got %d", len(state.Networks))
	}
	if len(state.Volumes) != 0 {
		t.Errorf("expected 0 volumes, got %d", len(state.Volumes))
	}
}

func TestGetStackState_FiltersOtherStacks(t *testing.T) {
	t.Parallel()

	myStackSvc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name: "mystack_web",
				Labels: map[string]string{
					"com.docker.stack.namespace": "mystack",
				},
			},
		},
	}
	otherStackSvc := swarm.Service{
		ID: "svc2",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name: "otherstack_web",
				Labels: map[string]string{
					"com.docker.stack.namespace": "otherstack",
				},
			},
		},
	}

	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, opts dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			filterValues := opts.Filters.Get("label")
			for _, value := range filterValues {
				if strings.Contains(value, "com.docker.stack.namespace=mystack") {
					return []swarm.Service{myStackSvc}, nil
				}
			}
			return []swarm.Service{myStackSvc, otherStackSvc}, nil
		},
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
	}

	state, err := swarmint.GetStackState(context.Background(), mock, "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := state.Services["web"]; !ok {
		t.Error("expected service key 'web' in state")
	}
	if _, ok := state.Services["otherstack_web"]; ok {
		t.Error("other stack service should not be included")
	}
}

func TestGetStackState_ServiceNameStripsPrefix(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return []swarm.Service{
				{Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_api"}}},
				{Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_db"}}},
			}, nil
		},
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
	}

	state, err := swarmint.GetStackState(context.Background(), mock, "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := state.Services["api"]; !ok {
		t.Error("expected key 'api'")
	}
	if _, ok := state.Services["db"]; !ok {
		t.Error("expected key 'db'")
	}
	if _, ok := state.Services["mystack_api"]; ok {
		t.Error("key should not contain stack prefix")
	}
}

func TestGetStackState_StripsPrefixForNetworksAndVolumes(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, nil
		},
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return []network.Summary{
				{Name: "mystack_backend"},
				{Name: "mystack_frontend"},
			}, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{
				Volumes: []*volume.Volume{
					{Name: "mystack_db"},
					{Name: "mystack_cache"},
				},
			}, nil
		},
	}

	state, err := swarmint.GetStackState(context.Background(), mock, "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := state.Networks["backend"]; !ok {
		t.Error("expected normalized network key 'backend'")
	}
	if _, ok := state.Networks["mystack_backend"]; ok {
		t.Error("network key should not contain stack prefix")
	}
	if _, ok := state.Volumes["db"]; !ok {
		t.Error("expected normalized volume key 'db'")
	}
	if _, ok := state.Volumes["mystack_db"]; ok {
		t.Error("volume key should not contain stack prefix")
	}
}

func TestGetStackState_ServiceListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("service list failed")
	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, wantErr
		},
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
	}

	_, err := swarmint.GetStackState(context.Background(), mock, "mystack")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped service list error, got %v", err)
	}
}

func TestGetStackState_NetworkListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("network list failed")
	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, nil
		},
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, wantErr
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
	}

	_, err := swarmint.GetStackState(context.Background(), mock, "mystack")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped network list error, got %v", err)
	}
}

func TestGetStackState_VolumeListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("volume list failed")
	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, nil
		},
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, wantErr
		},
	}

	_, err := swarmint.GetStackState(context.Background(), mock, "mystack")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped volume list error, got %v", err)
	}
}
