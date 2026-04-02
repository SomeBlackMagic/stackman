package health

import (
	"context"
	"fmt"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	dockerswarm "github.com/docker/docker/api/types/swarm"

	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

const defaultHealthPollInterval = 50 * time.Millisecond

// ServiceUpdateMonitor waits until all desired service tasks are running.
type ServiceUpdateMonitor struct {
	client       swarmint.DockerClient
	serviceID    string
	serviceName  string
	pollInterval time.Duration
}

// NewServiceUpdateMonitor constructs a monitor for a deployed service.
func NewServiceUpdateMonitor(client swarmint.DockerClient, serviceID, serviceName string) *ServiceUpdateMonitor {
	return &ServiceUpdateMonitor{
		client:       client,
		serviceID:    serviceID,
		serviceName:  serviceName,
		pollInterval: defaultHealthPollInterval,
	}
}

// WaitHealthy waits until all desired replicas become running.
func (m *ServiceUpdateMonitor) WaitHealthy(ctx context.Context, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	healthy, err := m.checkHealth(waitCtx)
	if err != nil {
		return fmt.Errorf("service %q: health check failed: %w", m.serviceName, err)
	}
	if healthy {
		return nil
	}

	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			if waitCtx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("service %q: timeout waiting for healthy state", m.serviceName)
			}
			return fmt.Errorf("service %q: waiting for healthy state canceled: %w", m.serviceName, waitCtx.Err())
		case <-ticker.C:
			healthy, err := m.checkHealth(waitCtx)
			if err != nil {
				return fmt.Errorf("service %q: health check failed: %w", m.serviceName, err)
			}
			if healthy {
				return nil
			}
		}
	}
}

func (m *ServiceUpdateMonitor) checkHealth(ctx context.Context) (bool, error) {
	tasks, err := m.client.TaskList(ctx, dockertypes.TaskListOptions{
		Filters: filters.NewArgs(
			filters.Arg("service", m.serviceID),
			filters.Arg("desired-state", string(dockerswarm.TaskStateRunning)),
		),
	})
	if err != nil {
		return false, fmt.Errorf("list tasks: %w", err)
	}

	svc, _, err := m.client.ServiceInspectWithRaw(ctx, m.serviceID, dockertypes.ServiceInspectOptions{})
	if err != nil {
		return false, fmt.Errorf("inspect service: %w", err)
	}

	desired := desiredTaskCount(svc)
	if desired == 0 {
		return true, nil
	}

	var running uint64
	for _, task := range tasks {
		switch task.Status.State {
		case dockerswarm.TaskStateFailed, dockerswarm.TaskStateRejected:
			if task.Status.Err != "" {
				return false, fmt.Errorf("task %s failed: %s", task.ID, task.Status.Err)
			}
			return false, fmt.Errorf("task %s failed", task.ID)
		case dockerswarm.TaskStateRunning:
			running++
		}
	}

	return running >= desired, nil
}

func desiredTaskCount(service dockerswarm.Service) uint64 {
	if service.Spec.Mode.Replicated != nil && service.Spec.Mode.Replicated.Replicas != nil {
		return *service.Spec.Mode.Replicated.Replicas
	}
	if service.Spec.Mode.Global != nil {
		return 1
	}
	return 1
}
