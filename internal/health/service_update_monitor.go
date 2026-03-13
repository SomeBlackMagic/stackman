package health

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
)

var (
	ErrServiceUpdatePaused     = errors.New("service update paused")
	ErrServiceUpdateRolledBack = errors.New("service update rolled back")
	ErrServiceRollbackPaused   = errors.New("service rollback paused")
)

// ServiceUpdateMonitor monitors service update status by polling ServiceInspect
// This is separate from task monitoring - it tracks overall service update progress
type ServiceUpdateMonitor struct {
	client      DockerClient
	logger      Logger
	serviceID   string
	serviceName string
}

// NewServiceUpdateMonitor creates a monitor for service update status
func NewServiceUpdateMonitor(client DockerClient, serviceID string, serviceName string) *ServiceUpdateMonitor {
	return NewServiceUpdateMonitorWithLogger(client, serviceID, serviceName, nil)
}

// NewServiceUpdateMonitorWithLogger creates a monitor for service update status with custom logger.
func NewServiceUpdateMonitorWithLogger(client DockerClient, serviceID string, serviceName string, logger Logger) *ServiceUpdateMonitor {
	return &ServiceUpdateMonitor{
		client:      client,
		logger:      withDefaultLogger(logger),
		serviceID:   serviceID,
		serviceName: serviceName,
	}
}

// WaitForUpdateComplete blocks until service update reaches terminal state
// Returns nil if update completed successfully, error otherwise
func (m *ServiceUpdateMonitor) WaitForUpdateComplete(ctx context.Context) error {
	m.logf("[ServiceUpdateMonitor] Waiting for service %s update to complete...", m.serviceName)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			svc, _, err := m.client.ServiceInspectWithRaw(ctx, m.serviceID, types.ServiceInspectOptions{})
			if err != nil {
				m.logf("[ServiceUpdateMonitor] Failed to inspect service %s: %v", m.serviceName, err)
				continue
			}

			// Check if update status is available
			if svc.UpdateStatus == nil {
				// No update in progress - service might be freshly created
				m.logf("[ServiceUpdateMonitor] No update status for service %s (might be new service)", m.serviceName)
				return nil
			}

			state := svc.UpdateStatus.State
			message := svc.UpdateStatus.Message

			// Log current state
			if message != "" {
				m.logf("[ServiceUpdateMonitor] Service %s update state: %s | Message: %s",
					m.serviceName, state, message)
			} else {
				m.logf("[ServiceUpdateMonitor] Service %s update state: %s",
					m.serviceName, state)
			}

			// Check terminal states
			switch state {
			case swarm.UpdateStateCompleted:
				m.logf("[ServiceUpdateMonitor] ✅ Service %s update completed successfully", m.serviceName)
				return nil

			case swarm.UpdateStatePaused:
				m.logf("[ServiceUpdateMonitor] ⏸️  Service %s update paused", m.serviceName)
				return fmt.Errorf("%w: %s", ErrServiceUpdatePaused, message)

			case swarm.UpdateStateRollbackCompleted:
				m.logf("[ServiceUpdateMonitor] 🔄 Service %s rollback completed", m.serviceName)
				return fmt.Errorf("%w: %s", ErrServiceUpdateRolledBack, message)

			case swarm.UpdateStateRollbackPaused:
				m.logf("[ServiceUpdateMonitor] ⏸️  Service %s rollback paused", m.serviceName)
				return fmt.Errorf("%w: %s", ErrServiceRollbackPaused, message)

			case swarm.UpdateStateUpdating:
				// Still in progress, continue waiting
				m.logf("[ServiceUpdateMonitor] 🔄 Service %s is updating...", m.serviceName)

			case swarm.UpdateStateRollbackStarted:
				m.logf("[ServiceUpdateMonitor] 🔄 Service %s rollback started", m.serviceName)

			default:
				m.logf("[ServiceUpdateMonitor] ⚠️  Service %s unknown update state: %s", m.serviceName, state)
			}
		}
	}
}

// GetUpdateStatus returns current update status without blocking
func (m *ServiceUpdateMonitor) GetUpdateStatus(ctx context.Context) (*swarm.UpdateStatus, error) {
	svc, _, err := m.client.ServiceInspectWithRaw(ctx, m.serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect service: %w", err)
	}

	return svc.UpdateStatus, nil
}

func (m *ServiceUpdateMonitor) logf(format string, args ...any) {
	m.logger.Printf(format, args...)
}
