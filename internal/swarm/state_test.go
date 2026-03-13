package swarm

import (
	"context"
	"io"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

// mockDockerClient implements DockerClient for testing
type mockStateDockerClient struct {
	services []swarm.Service
	networks []network.Summary
	volumes  []*volume.Volume
}

func (m *mockStateDockerClient) ServiceList(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error) {
	return m.services, nil
}

func (m *mockStateDockerClient) NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error) {
	return m.networks, nil
}

func (m *mockStateDockerClient) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	for _, net := range m.networks {
		if net.ID == networkID {
			return network.Inspect{
				ID:      net.ID,
				Name:    net.Name,
				Driver:  net.Driver,
				Labels:  net.Labels,
				Options: net.Options,
			}, nil
		}
	}
	return network.Inspect{}, nil
}

func (m *mockStateDockerClient) VolumeList(ctx context.Context, options volume.ListOptions) (volume.ListResponse, error) {
	return volume.ListResponse{
		Volumes: m.volumes,
	}, nil
}

// Stub implementations for interface compliance
func (m *mockStateDockerClient) ServiceCreate(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
	return swarm.ServiceCreateResponse{}, nil
}
func (m *mockStateDockerClient) ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
	return swarm.ServiceUpdateResponse{}, nil
}
func (m *mockStateDockerClient) ServiceRemove(ctx context.Context, serviceID string) error {
	return nil
}
func (m *mockStateDockerClient) ServiceInspectWithRaw(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error) {
	return swarm.Service{}, nil, nil
}
func (m *mockStateDockerClient) TaskList(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error) {
	return nil, nil
}
func (m *mockStateDockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	return nil, nil
}
func (m *mockStateDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	return types.ContainerJSON{}, nil
}
func (m *mockStateDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	return nil
}
func (m *mockStateDockerClient) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{}, nil
}
func (m *mockStateDockerClient) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	return volume.Volume{}, nil
}
func (m *mockStateDockerClient) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	return volume.Volume{}, nil
}
func (m *mockStateDockerClient) NetworkRemove(ctx context.Context, networkID string) error {
	return nil
}
func (m *mockStateDockerClient) NodeList(ctx context.Context, options swarm.NodeListOptions) ([]swarm.Node, error) {
	return nil, nil
}
func (m *mockStateDockerClient) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockStateDockerClient) Close() error { return nil }

func TestGetCurrentState(t *testing.T) {
	stackName := "test-stack"

	mockCli := &mockStateDockerClient{
		services: []swarm.Service{
			{
				ID: "service1",
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{
						Name: stackName + "_web",
						Labels: map[string]string{
							"com.docker.stack.namespace": stackName,
						},
					},
				},
			},
			{
				ID: "service2",
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{
						Name: stackName + "_api",
						Labels: map[string]string{
							"com.docker.stack.namespace": stackName,
						},
					},
				},
			},
		},
		networks: []network.Summary{
			{
				ID:     "net1",
				Name:   stackName + "_frontend",
				Driver: "overlay",
				Labels: map[string]string{
					"com.docker.stack.namespace": stackName,
				},
			},
		},
		volumes: []*volume.Volume{
			{
				Name:   stackName + "_data",
				Driver: "local",
				Labels: map[string]string{
					"com.docker.stack.namespace": stackName,
				},
			},
		},
	}

	ctx := context.Background()
	state, err := GetCurrentState(ctx, mockCli, stackName)
	if err != nil {
		t.Fatalf("GetCurrentState failed: %v", err)
	}

	// Check services
	if len(state.Services) != 2 {
		t.Errorf("Expected 2 services, got %d", len(state.Services))
	}
	if _, exists := state.Services["web"]; !exists {
		t.Error("Expected service 'web' to exist")
	}
	if _, exists := state.Services["api"]; !exists {
		t.Error("Expected service 'api' to exist")
	}

	// Check networks
	if len(state.Networks) != 1 {
		t.Errorf("Expected 1 network, got %d", len(state.Networks))
	}
	if _, exists := state.Networks["frontend"]; !exists {
		t.Error("Expected network 'frontend' to exist")
	}

	// Check volumes
	if len(state.Volumes) != 1 {
		t.Errorf("Expected 1 volume, got %d", len(state.Volumes))
	}
	if _, exists := state.Volumes["data"]; !exists {
		t.Error("Expected volume 'data' to exist")
	}
}

func TestGetCurrentState_EmptyStack(t *testing.T) {
	stackName := "empty-stack"

	mockCli := &mockStateDockerClient{
		services: []swarm.Service{},
		networks: []network.Summary{},
		volumes:  []*volume.Volume{},
	}

	ctx := context.Background()
	state, err := GetCurrentState(ctx, mockCli, stackName)
	if err != nil {
		t.Fatalf("GetCurrentState failed: %v", err)
	}

	if len(state.Services) != 0 {
		t.Errorf("Expected 0 services, got %d", len(state.Services))
	}
	if len(state.Networks) != 0 {
		t.Errorf("Expected 0 networks, got %d", len(state.Networks))
	}
	if len(state.Volumes) != 0 {
		t.Errorf("Expected 0 volumes, got %d", len(state.Volumes))
	}
}
