package swarm

import (
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

// ServiceClient defines service-related Docker API methods.
type ServiceClient interface {
	ServiceList(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error)
	ServiceCreate(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error)
	ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error)
	ServiceRemove(ctx context.Context, serviceID string) error
	ServiceInspectWithRaw(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error)
}

// TaskClient defines task-related Docker API methods.
type TaskClient interface {
	TaskList(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error)
}

// ContainerClient defines container-related Docker API methods.
type ContainerClient interface {
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
}

// NetworkClient defines network-related Docker API methods.
type NetworkClient interface {
	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
	NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
	NetworkRemove(ctx context.Context, networkID string) error
}

// VolumeClient defines volume-related Docker API methods.
type VolumeClient interface {
	VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
	VolumeList(ctx context.Context, options volume.ListOptions) (volume.ListResponse, error)
	VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error)
}

// NodeClient defines swarm node-related methods.
type NodeClient interface {
	NodeList(ctx context.Context, options swarm.NodeListOptions) ([]swarm.Node, error)
}

// ImageClient defines image-related methods.
type ImageClient interface {
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
}

// CloseClient defines resource cleanup method.
type CloseClient interface {
	Close() error
}

// DeploymentClient is the minimal composite interface required by StackDeployer.
type DeploymentClient interface {
	ServiceClient
	TaskClient
	ContainerClient
	NetworkClient
	VolumeClient
	NodeClient
	ImageClient
}

// StateClient is the minimal interface required for reading stack state.
type StateClient interface {
	ServiceClient
	NetworkClient
	VolumeClient
}

// DockerClient is kept for compatibility with existing call sites and tests.
type DockerClient interface {
	DeploymentClient
	StateClient
	CloseClient
}
