package swarm

import (
	"context"
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

// DockerClient abstracts Docker API access for stack management use cases.
type DockerClient interface {
	// Services
	ServiceList(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error)
	ServiceCreate(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error)
	ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error)
	ServiceRemove(ctx context.Context, serviceID string) error
	ServiceInspectWithRaw(ctx context.Context, serviceID string, opts types.ServiceInspectOptions) (swarm.Service, []byte, error)

	// Tasks
	TaskList(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error)

	// Containers
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)

	// Networks
	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
	NetworkRemove(ctx context.Context, networkID string) error

	// Volumes
	VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
	VolumeList(ctx context.Context, filter filters.Args) (volume.ListResponse, error)
	VolumeRemove(ctx context.Context, volumeID string, force bool) error

	// Events
	Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error)

	// Nodes
	NodeList(ctx context.Context, options types.NodeListOptions) ([]swarm.Node, error)

	// Images
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)

	Close() error
}
