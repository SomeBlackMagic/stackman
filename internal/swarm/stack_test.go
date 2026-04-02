package swarm_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/SomeBlackMagic/stackman/internal/compose"
	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestStackDeployer_Deploy_PositiveCaseStepOrder(t *testing.T) {
	t.Parallel()

	steps := make([]string, 0, 8)

	mock := &swarmint.MockDockerClient{
		ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]dockertypes.Container, error) {
			steps = append(steps, "container_list")
			return nil, nil
		},
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			steps = append(steps, "service_list")
			return nil, nil
		},
		ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
			steps = append(steps, "image_pull")
			return io.NopCloser(strings.NewReader("{}\n")), nil
		},
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			steps = append(steps, "network_list")
			return nil, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			steps = append(steps, "volume_list")
			return volume.ListResponse{}, nil
		},
		ServiceCreateFn: func(_ context.Context, _ swarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
			steps = append(steps, "service_create")
			return swarm.ServiceCreateResponse{ID: "svc1"}, nil
		},
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "n1"}}, nil
		},
	}

	cf := &compose.ComposeFile{
		Services: map[string]*compose.Service{
			"web": {Image: "nginx:latest"},
		},
	}

	deployer := swarmint.NewStackDeployer(mock, "mystack")
	_, err := deployer.Deploy(context.Background(), cf, "deploy-id", swarmint.DeployOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pos := func(step string) int {
		for i, current := range steps {
			if current == step {
				return i
			}
		}
		return -1
	}

	orderChecks := []struct {
		before string
		after  string
	}{
		{before: "container_list", after: "image_pull"},
		{before: "image_pull", after: "network_list"},
		{before: "network_list", after: "volume_list"},
		{before: "volume_list", after: "service_create"},
	}

	for _, check := range orderChecks {
		beforePos := pos(check.before)
		afterPos := pos(check.after)
		if beforePos == -1 || afterPos == -1 {
			t.Fatalf("missing step in trace: before=%q pos=%d after=%q pos=%d steps=%v", check.before, beforePos, check.after, afterPos, steps)
		}
		if beforePos > afterPos {
			t.Fatalf("step %q should come before %q, got steps=%v", check.before, check.after, steps)
		}
	}
}

func TestStackDeployer_Deploy_NegativeCaseStopsOnPullError(t *testing.T) {
	t.Parallel()

	serviceCreateCalled := 0

	mock := &swarmint.MockDockerClient{
		ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]dockertypes.Container, error) {
			return nil, nil
		},
		ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
			return nil, errors.New("image not found")
		},
		ServiceCreateFn: func(_ context.Context, _ swarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
			serviceCreateCalled++
			return swarm.ServiceCreateResponse{}, nil
		},
	}

	cf := &compose.ComposeFile{
		Services: map[string]*compose.Service{
			"web": {Image: "nonexistent:latest"},
		},
	}

	deployer := swarmint.NewStackDeployer(mock, "mystack")
	_, err := deployer.Deploy(context.Background(), cf, "deploy-id", swarmint.DeployOptions{})
	if err == nil {
		t.Fatal("expected pull images error")
	}
	if serviceCreateCalled != 0 {
		t.Fatalf("ServiceCreate should not be called after PullImages fails, got %d", serviceCreateCalled)
	}
	if !strings.Contains(err.Error(), "pull images") {
		t.Fatalf("error should include pull context, got: %v", err)
	}
}

func TestStackDeployer_Deploy_NegativeCaseErrorWrapping(t *testing.T) {
	t.Parallel()

	baseErr := errors.New("docker connection refused")
	mock := &swarmint.MockDockerClient{
		ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]dockertypes.Container, error) {
			return nil, baseErr
		},
	}

	deployer := swarmint.NewStackDeployer(mock, "mystack")
	cf := &compose.ComposeFile{}
	_, err := deployer.Deploy(context.Background(), cf, "deploy-id", swarmint.DeployOptions{})
	if !errors.Is(err, baseErr) {
		t.Fatalf("expected wrapped base error, got: %v", err)
	}
}

func TestStackDeployer_Deploy_PositiveCasePruneOrphans(t *testing.T) {
	t.Parallel()

	serviceListCalled := 0
	networkListCalled := 0

	mock := &swarmint.MockDockerClient{
		ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]dockertypes.Container, error) {
			return nil, nil
		},
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			serviceListCalled++
			return nil, nil
		},
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			networkListCalled++
			return []network.Summary{
				{ID: "net1", Name: "mystack_default"},
			}, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
		ServiceCreateFn: func(_ context.Context, _ swarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
			return swarm.ServiceCreateResponse{ID: "svc1"}, nil
		},
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "n1"}}, nil
		},
	}

	cf := &compose.ComposeFile{
		Services: map[string]*compose.Service{
			"web": {Image: "nginx:latest"},
		},
		Networks: map[string]*compose.Network{
			"default": {},
		},
	}

	deployer := swarmint.NewStackDeployer(mock, "mystack")
	_, err := deployer.Deploy(context.Background(), cf, "deploy-id", swarmint.DeployOptions{
		PruneOrphans: true,
		NoPull:       true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if serviceListCalled < 2 {
		t.Fatalf("expected ServiceList to be called by prune + deploy, got %d", serviceListCalled)
	}
	if networkListCalled < 2 {
		t.Fatalf("expected NetworkList to be called by prune + ensure, got %d", networkListCalled)
	}
}

func TestStackDeployer_Deploy_EdgeCaseNoPull(t *testing.T) {
	t.Parallel()

	imagePullCalled := 0

	mock := &swarmint.MockDockerClient{
		ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]dockertypes.Container, error) {
			return nil, nil
		},
		ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
			imagePullCalled++
			return io.NopCloser(strings.NewReader("{}\n")), nil
		},
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, nil
		},
		ServiceCreateFn: func(_ context.Context, _ swarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
			return swarm.ServiceCreateResponse{ID: "svc1"}, nil
		},
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "n1"}}, nil
		},
	}

	cf := &compose.ComposeFile{
		Services: map[string]*compose.Service{
			"web": {Image: "nginx:latest"},
		},
	}

	deployer := swarmint.NewStackDeployer(mock, "mystack")
	_, err := deployer.Deploy(context.Background(), cf, "deploy-id", swarmint.DeployOptions{
		NoPull: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if imagePullCalled != 0 {
		t.Fatalf("expected ImagePull not to be called when NoPull=true, got %d", imagePullCalled)
	}
}

func TestStackDeployer_GetState_PositiveCase(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, opts dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			labels := opts.Filters.Get("label")
			if len(labels) != 1 || labels[0] != "com.docker.stack.namespace=mystack" {
				t.Fatalf("unexpected service list labels: %v", labels)
			}
			return []swarm.Service{
				{
					ID: "svc1",
					Spec: swarm.ServiceSpec{
						Annotations: swarm.Annotations{Name: "mystack_web"},
					},
				},
			}, nil
		},
		NetworkListFn: func(_ context.Context, opts network.ListOptions) ([]network.Summary, error) {
			labels := opts.Filters.Get("label")
			if len(labels) != 1 || labels[0] != "com.docker.stack.namespace=mystack" {
				t.Fatalf("unexpected network list labels: %v", labels)
			}
			return []network.Summary{
				{ID: "net1", Name: "mystack_default"},
			}, nil
		},
		VolumeListFn: func(_ context.Context, filter filters.Args) (volume.ListResponse, error) {
			labels := filter.Get("label")
			if len(labels) != 1 || labels[0] != "com.docker.stack.namespace=mystack" {
				t.Fatalf("unexpected volume list labels: %v", labels)
			}
			return volume.ListResponse{
				Volumes: []*volume.Volume{
					{Name: "mystack_data"},
				},
			}, nil
		},
	}

	deployer := swarmint.NewStackDeployer(mock, "mystack")
	state, err := deployer.GetState(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if _, ok := state.Services["web"]; !ok {
		t.Fatalf("expected normalized service name key 'web', got: %v", state.Services)
	}
	if _, ok := state.Networks["default"]; !ok {
		t.Fatalf("expected normalized network name key 'default', got: %v", state.Networks)
	}
	if _, ok := state.Volumes["data"]; !ok {
		t.Fatalf("expected normalized volume name key 'data', got: %v", state.Volumes)
	}
}
