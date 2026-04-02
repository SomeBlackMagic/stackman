package swarm

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

// MockDockerClient is a test double for DockerClient.
// Every method delegates to its Fn field and panics when a required Fn is unset.
type MockDockerClient struct {
	ServiceListFn           func(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error)
	ServiceCreateFn         func(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error)
	ServiceUpdateFn         func(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error)
	ServiceRemoveFn         func(ctx context.Context, serviceID string) error
	ServiceInspectWithRawFn func(ctx context.Context, serviceID string, opts types.ServiceInspectOptions) (swarm.Service, []byte, error)

	TaskListFn func(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error)

	ContainerListFn    func(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	ContainerInspectFn func(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerRemoveFn  func(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerLogsFn    func(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)

	NetworkCreateFn func(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	NetworkListFn   func(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
	NetworkRemoveFn func(ctx context.Context, networkID string) error

	VolumeCreateFn func(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
	VolumeListFn   func(ctx context.Context, filter filters.Args) (volume.ListResponse, error)
	VolumeRemoveFn func(ctx context.Context, volumeID string, force bool) error

	EventsFn    func(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error)
	NodeListFn  func(ctx context.Context, options types.NodeListOptions) ([]swarm.Node, error)
	ImagePullFn func(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
	CloseFn     func() error
}

var _ DockerClient = (*MockDockerClient)(nil)

func mustSet(method string) {
	panic(fmt.Sprintf("MockDockerClient.%s is not set", method))
}

func (m *MockDockerClient) ServiceList(ctx context.Context, opts types.ServiceListOptions) ([]swarm.Service, error) {
	if m.ServiceListFn == nil {
		mustSet("ServiceList")
	}
	return m.ServiceListFn(ctx, opts)
}

func (m *MockDockerClient) ServiceCreate(ctx context.Context, service swarm.ServiceSpec, opts types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
	if m.ServiceCreateFn == nil {
		mustSet("ServiceCreate")
	}
	return m.ServiceCreateFn(ctx, service, opts)
}

func (m *MockDockerClient) ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, opts types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
	if m.ServiceUpdateFn == nil {
		mustSet("ServiceUpdate")
	}
	return m.ServiceUpdateFn(ctx, serviceID, version, service, opts)
}

func (m *MockDockerClient) ServiceRemove(ctx context.Context, serviceID string) error {
	if m.ServiceRemoveFn == nil {
		mustSet("ServiceRemove")
	}
	return m.ServiceRemoveFn(ctx, serviceID)
}

func (m *MockDockerClient) ServiceInspectWithRaw(ctx context.Context, serviceID string, opts types.ServiceInspectOptions) (swarm.Service, []byte, error) {
	if m.ServiceInspectWithRawFn == nil {
		mustSet("ServiceInspectWithRaw")
	}
	return m.ServiceInspectWithRawFn(ctx, serviceID, opts)
}

func (m *MockDockerClient) TaskList(ctx context.Context, opts types.TaskListOptions) ([]swarm.Task, error) {
	if m.TaskListFn == nil {
		mustSet("TaskList")
	}
	return m.TaskListFn(ctx, opts)
}

func (m *MockDockerClient) ContainerList(ctx context.Context, opts container.ListOptions) ([]types.Container, error) {
	if m.ContainerListFn == nil {
		mustSet("ContainerList")
	}
	return m.ContainerListFn(ctx, opts)
}

func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	if m.ContainerInspectFn == nil {
		mustSet("ContainerInspect")
	}
	return m.ContainerInspectFn(ctx, containerID)
}

func (m *MockDockerClient) ContainerRemove(ctx context.Context, containerID string, opts container.RemoveOptions) error {
	if m.ContainerRemoveFn == nil {
		mustSet("ContainerRemove")
	}
	return m.ContainerRemoveFn(ctx, containerID, opts)
}

func (m *MockDockerClient) ContainerLogs(ctx context.Context, containerID string, opts container.LogsOptions) (io.ReadCloser, error) {
	if m.ContainerLogsFn == nil {
		mustSet("ContainerLogs")
	}
	return m.ContainerLogsFn(ctx, containerID, opts)
}

func (m *MockDockerClient) NetworkCreate(ctx context.Context, name string, opts network.CreateOptions) (network.CreateResponse, error) {
	if m.NetworkCreateFn == nil {
		mustSet("NetworkCreate")
	}
	return m.NetworkCreateFn(ctx, name, opts)
}

func (m *MockDockerClient) NetworkList(ctx context.Context, opts network.ListOptions) ([]network.Summary, error) {
	if m.NetworkListFn == nil {
		mustSet("NetworkList")
	}
	return m.NetworkListFn(ctx, opts)
}

func (m *MockDockerClient) NetworkRemove(ctx context.Context, networkID string) error {
	if m.NetworkRemoveFn == nil {
		mustSet("NetworkRemove")
	}
	return m.NetworkRemoveFn(ctx, networkID)
}

func (m *MockDockerClient) VolumeCreate(ctx context.Context, opts volume.CreateOptions) (volume.Volume, error) {
	if m.VolumeCreateFn == nil {
		mustSet("VolumeCreate")
	}
	return m.VolumeCreateFn(ctx, opts)
}

func (m *MockDockerClient) VolumeList(ctx context.Context, filter filters.Args) (volume.ListResponse, error) {
	if m.VolumeListFn == nil {
		mustSet("VolumeList")
	}
	return m.VolumeListFn(ctx, filter)
}

func (m *MockDockerClient) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	if m.VolumeRemoveFn == nil {
		mustSet("VolumeRemove")
	}
	return m.VolumeRemoveFn(ctx, volumeID, force)
}

func (m *MockDockerClient) Events(ctx context.Context, opts events.ListOptions) (<-chan events.Message, <-chan error) {
	if m.EventsFn == nil {
		mustSet("Events")
	}
	return m.EventsFn(ctx, opts)
}

func (m *MockDockerClient) NodeList(ctx context.Context, opts types.NodeListOptions) ([]swarm.Node, error) {
	if m.NodeListFn == nil {
		mustSet("NodeList")
	}
	return m.NodeListFn(ctx, opts)
}

func (m *MockDockerClient) ImagePull(ctx context.Context, refStr string, opts image.PullOptions) (io.ReadCloser, error) {
	if m.ImagePullFn == nil {
		mustSet("ImagePull")
	}
	return m.ImagePullFn(ctx, refStr, opts)
}

func (m *MockDockerClient) Close() error {
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

// NewEmptyStackMock returns a mock configured for scenarios where a stack does not exist yet.
func NewEmptyStackMock() *MockDockerClient {
	return &MockDockerClient{
		ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarm.Service, error) {
			return nil, nil
		},
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
		NodeListFn: func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "node1"}}, nil
		},
	}
}
