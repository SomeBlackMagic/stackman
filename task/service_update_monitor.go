package task

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

// ServiceUpdateMonitor monitors service update status by polling ServiceInspect
// This is separate from task monitoring - it tracks overall service update progress
type ServiceUpdateMonitor struct {
	client      client.APIClient
	serviceID   string
	serviceName string
}

// NewServiceUpdateMonitor creates a monitor for service update status
func NewServiceUpdateMonitor(client client.APIClient, serviceID string, serviceName string) *ServiceUpdateMonitor {
	return &ServiceUpdateMonitor{
		client:      client,
		serviceID:   serviceID,
		serviceName: serviceName,
	}
}

// WaitForUpdateComplete blocks until service update reaches terminal state
// Returns nil if update completed successfully, error otherwise
func (m *ServiceUpdateMonitor) WaitForUpdateComplete(ctx context.Context) error {
	log.Printf("[ServiceUpdateMonitor] Waiting for service %s update to complete...", m.serviceName)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			svc, _, err := m.client.ServiceInspectWithRaw(ctx, m.serviceID, types.ServiceInspectOptions{})
			if err != nil {
				log.Printf("[ServiceUpdateMonitor] Failed to inspect service %s: %v", m.serviceName, err)
				continue
			}

			// Check if update status is available
			if svc.UpdateStatus == nil {
				// No update in progress - service might be freshly created
				log.Printf("[ServiceUpdateMonitor] No update status for service %s (might be new service)", m.serviceName)
				return nil
			}

			state := svc.UpdateStatus.State
			message := svc.UpdateStatus.Message

			// Log current state
			if message != "" {
				log.Printf("[ServiceUpdateMonitor] Service %s update state: %s | Message: %s",
					m.serviceName, state, message)
			} else {
				log.Printf("[ServiceUpdateMonitor] Service %s update state: %s",
					m.serviceName, state)
			}

			// Check terminal states
			switch state {
			case swarm.UpdateStateCompleted:
				log.Printf("[ServiceUpdateMonitor] ✅ Service %s update completed successfully", m.serviceName)
				return nil

			case swarm.UpdateStatePaused:
				log.Printf("[ServiceUpdateMonitor] ⏸️  Service %s update paused", m.serviceName)
				return fmt.Errorf("service update paused: %s", message)

			case swarm.UpdateStateRollbackCompleted:
				log.Printf("[ServiceUpdateMonitor] 🔄 Service %s rollback completed", m.serviceName)
				return fmt.Errorf("service update rolled back: %s", message)

			case swarm.UpdateStateRollbackPaused:
				log.Printf("[ServiceUpdateMonitor] ⏸️  Service %s rollback paused", m.serviceName)
				return fmt.Errorf("service rollback paused: %s", message)

			case swarm.UpdateStateUpdating:
				// Still in progress, continue waiting
				log.Printf("[ServiceUpdateMonitor] 🔄 Service %s is updating...", m.serviceName)

			case swarm.UpdateStateRollbackStarted:
				log.Printf("[ServiceUpdateMonitor] 🔄 Service %s rollback started", m.serviceName)

			default:
				log.Printf("[ServiceUpdateMonitor] ⚠️  Service %s unknown update state: %s", m.serviceName, state)
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
