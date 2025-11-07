package swarm

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

// MockDockerClient implements DockerClient interface for testing
type MockDockerClient struct {
	services        []swarm.Service
	tasks           []swarm.Task
	containers      []types.Container
	networks        []network.Summary
	volumes         []volume.Volume
	removedServices []string
	updatedServices []string
	createdServices []swarm.Service
}

func (m *MockDockerClient) ServiceList(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error) {
	return m.services, nil
}

func (m *MockDockerClient) ServiceCreate(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
	newService := swarm.Service{
		ID:   fmt.Sprintf("service_%d", len(m.createdServices)+1),
		Spec: service,
	}
	m.createdServices = append(m.createdServices, newService)
	return swarm.ServiceCreateResponse{ID: newService.ID}, nil
}

func (m *MockDockerClient) ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
	m.updatedServices = append(m.updatedServices, serviceID)
	return swarm.ServiceUpdateResponse{}, nil
}

func (m *MockDockerClient) ServiceRemove(ctx context.Context, serviceID string) error {
	m.removedServices = append(m.removedServices, serviceID)
	return nil
}

func (m *MockDockerClient) ServiceInspectWithRaw(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error) {
	for _, svc := range m.services {
		if svc.ID == serviceID {
			return svc, nil, nil
		}
	}
	return swarm.Service{}, nil, fmt.Errorf("service not found: %s", serviceID)
}

func (m *MockDockerClient) TaskList(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error) {
	return m.tasks, nil
}

func (m *MockDockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	return m.containers, nil
}

func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	return types.ContainerJSON{}, nil
}

func (m *MockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	return nil
}

func (m *MockDockerClient) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{ID: "network_" + name}, nil
}

func (m *MockDockerClient) NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error) {
	return m.networks, nil
}

func (m *MockDockerClient) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	return network.Inspect{}, nil
}

func (m *MockDockerClient) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	return volume.Volume{Name: options.Name}, nil
}

func (m *MockDockerClient) VolumeList(ctx context.Context, options volume.ListOptions) (volume.ListResponse, error) {
	vols := make([]*volume.Volume, len(m.volumes))
	for i := range m.volumes {
		vols[i] = &m.volumes[i]
	}
	return volume.ListResponse{Volumes: vols}, nil
}

func (m *MockDockerClient) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	return volume.Volume{}, nil
}

func (m *MockDockerClient) NetworkRemove(ctx context.Context, networkID string) error {
	return nil
}

func (m *MockDockerClient) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	return io.NopCloser(nil), nil
}

func (m *MockDockerClient) Close() error {
	return nil
}
