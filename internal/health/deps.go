package health

import (
	"context"
	"io"
	"log"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/swarm"
)

// DockerClient defines the minimal Docker API surface used by health monitors.
type DockerClient interface {
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error)
	ServiceInspectWithRaw(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error)
	ServiceList(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error)
	TaskList(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error)
}

// Logger is a minimal logging abstraction for dependency injection.
type Logger interface {
	Printf(format string, v ...any)
}

type stdLogger struct{}

func (stdLogger) Printf(format string, v ...any) {
	log.Printf(format, v...)
}

func withDefaultLogger(logger Logger) Logger {
	if logger != nil {
		return logger
	}
	return stdLogger{}
}
