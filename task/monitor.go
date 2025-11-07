package task

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Monitor monitors a single task's lifecycle, health, and logs
// It automatically cleans up goroutines when task reaches terminal state
type Monitor struct {
	client client.APIClient

	// Task identification
	taskID      string
	serviceID   string
	serviceName string
	containerID string

	// Monitoring state
	state        string
	desiredState string
	lastSeen     time.Time

	// Health status
	healthStatus string // starting, healthy, unhealthy
	healthChecks int    // number of health checks performed
	failedChecks int    // number of failed health checks

	// Channels for coordination
	eventChan chan Event    // receives events for this task
	stopChan  chan struct{} // signals monitor to stop
	doneChan  chan struct{} // signals monitor has stopped

	// Lifecycle management
	ctx          context.Context
	cancel       context.CancelFunc
	shutdownOnce sync.Once

	// Thread safety
	mu sync.RWMutex
}

// NewMonitor creates a new task monitor
func NewMonitor(client client.APIClient, taskID string, serviceID string, serviceName string) *Monitor {
	ctx, cancel := context.WithCancel(context.Background())

	return &Monitor{
		client:       client,
		taskID:       taskID,
		serviceID:    serviceID,
		serviceName:  serviceName,
		eventChan:    make(chan Event, 10),
		stopChan:     make(chan struct{}),
		doneChan:     make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
		healthStatus: "unknown",
		lastSeen:     time.Now(),
	}
}

// Start begins monitoring the task
// This method blocks until task reaches terminal state or context is cancelled
func (m *Monitor) Start(ctx context.Context) error {
	defer close(m.doneChan)
	defer m.cleanup()

	log.Printf("[TaskMonitor] Started monitoring task %s (service: %s)", m.shortTaskID(), m.serviceName)

	// Start goroutines for different monitoring aspects
	var wg sync.WaitGroup

	// Health monitoring goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.monitorHealth(ctx)
	}()

	// Log streaming goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.streamLogs(ctx)
	}()

	// Event processing goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.processEvents(ctx)
	}()

	// Wait for completion
	select {
	case <-ctx.Done():
		log.Printf("[TaskMonitor] Context cancelled for task %s", m.shortTaskID())
		m.Stop()
	case <-m.stopChan:
		log.Printf("[TaskMonitor] Stop signal received for task %s", m.shortTaskID())
	}

	// Wait for all goroutines to finish
	wg.Wait()

	log.Printf("[TaskMonitor] Stopped monitoring task %s", m.shortTaskID())
	return nil
}

// Stop signals the monitor to stop
func (m *Monitor) Stop() {
	m.shutdownOnce.Do(func() {
		close(m.stopChan)
		m.cancel()
	})
}

// SendEvent sends an event to this task monitor
// Returns false if monitor is shutting down
func (m *Monitor) SendEvent(event Event) bool {
	select {
	case m.eventChan <- event:
		return true
	case <-m.stopChan:
		return false
	default:
		// Channel full, log warning
		log.Printf("[TaskMonitor] WARNING: Event channel full for task %s", m.shortTaskID())
		return false
	}
}

// processEvents processes task lifecycle events
func (m *Monitor) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return

		case event := <-m.eventChan:
			m.handleEvent(event)

			// If task reached terminal state, stop monitoring
			if event.IsTerminal() {
				log.Printf("[TaskMonitor] Task %s reached terminal state: %s",
					m.shortTaskID(), event.Type)
				m.Stop()
				return
			}
		}
	}
}

// handleEvent updates internal state based on event
func (m *Monitor) handleEvent(event Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastSeen = time.Now()
	m.state = event.State
	m.desiredState = event.DesiredState

	if event.ContainerID != "" && m.containerID == "" {
		m.containerID = event.ContainerID
	}

	// Update health status
	if event.IsHealthRelated() {
		m.healthChecks++
		if event.Type == EventTypeHealthy {
			m.healthStatus = "healthy"
		} else if event.Type == EventTypeUnhealthy {
			m.healthStatus = "unhealthy"
			m.failedChecks++
		}
	}

	// Log important events
	switch event.Type {
	case EventTypeCreated:
		containerInfo := ""
		if m.containerID != "" {
			containerInfo = fmt.Sprintf(" (container: %s)", m.containerID[:12])
		}
		log.Printf("[TaskMonitor] 🆕 Task %s created%s", m.shortTaskID(), containerInfo)
	case EventTypeStarted:
		log.Printf("[TaskMonitor] ▶️  Task %s started", m.shortTaskID())
	case EventTypeHealthy:
		log.Printf("[TaskMonitor] 💚 Task %s is healthy (checks: %d)",
			m.shortTaskID(), m.healthChecks)
	case EventTypeUnhealthy:
		log.Printf("[TaskMonitor] 💔 Task %s is unhealthy (failed: %d/%d)",
			m.shortTaskID(), m.failedChecks, m.healthChecks)
	case EventTypeFailed:
		log.Printf("[TaskMonitor] ❌ Task %s failed: %s", m.shortTaskID(), event.Message)
	case EventTypeCompleted:
		log.Printf("[TaskMonitor] 🏁 Task %s completed", m.shortTaskID())
	}
}

// monitorHealth periodically checks task health via Docker API
func (m *Monitor) monitorHealth(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.checkHealth(ctx)
		}
	}
}

// checkHealth queries Docker API for current task health
func (m *Monitor) checkHealth(ctx context.Context) {
	m.mu.RLock()
	containerID := m.containerID
	m.mu.RUnlock()

	if containerID == "" {
		// No container yet
		return
	}

	// Inspect container to get health status
	containerInfo, err := m.client.ContainerInspect(ctx, containerID)
	if err != nil {
		// Container might not exist anymore
		return
	}

	if containerInfo.State.Health != nil {
		m.mu.Lock()
		m.healthStatus = containerInfo.State.Health.Status
		m.mu.Unlock()
	}
}

// streamLogs streams container logs for this task
func (m *Monitor) streamLogs(ctx context.Context) {
	// Wait for container ID to be available
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		default:
		}

		m.mu.RLock()
		containerID := m.containerID
		m.mu.RUnlock()

		if containerID != "" {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	m.mu.RLock()
	containerID := m.containerID
	m.mu.RUnlock()

	// Start streaming logs
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
		Since:      time.Now().Format(time.RFC3339),
	}

	logReader, err := m.client.ContainerLogs(ctx, containerID, options)
	if err != nil {
		log.Printf("[TaskMonitor] Failed to stream logs for task %s: %v", m.shortTaskID(), err)
		return
	}
	defer logReader.Close()

	// Read logs until context cancelled or container stops
	// Note: In production, you'd want to parse the logs properly
	// For now, we just ensure the stream is active
	buf := make([]byte, 1024)
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		default:
		}

		_, err := logReader.Read(buf)
		if err != nil {
			// Log stream ended
			return
		}
	}
}

// cleanup performs cleanup when monitor stops
func (m *Monitor) cleanup() {
	log.Printf("[TaskMonitor] Cleaning up monitor for task %s", m.shortTaskID())
	close(m.eventChan)
}

// GetState returns current task state (thread-safe)
func (m *Monitor) GetState() (state, desiredState, health string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state, m.desiredState, m.healthStatus
}

// GetStats returns monitoring statistics
func (m *Monitor) GetStats() (healthChecks, failedChecks int, lastSeen time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.healthChecks, m.failedChecks, m.lastSeen
}

// IsHealthy returns true if task is currently healthy
func (m *Monitor) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.healthStatus == "healthy"
}

// WaitForHealthy waits until task becomes healthy or times out
func (m *Monitor) WaitForHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return fmt.Errorf("monitor stopped before task became healthy")
		case <-ticker.C:
			if m.IsHealthy() {
				return nil
			}

			if time.Now().After(deadline) {
				m.mu.RLock()
				status := m.healthStatus
				m.mu.RUnlock()
				return fmt.Errorf("timeout waiting for task to become healthy (status: %s)", status)
			}
		}
	}
}

// Done returns a channel that is closed when monitor stops
func (m *Monitor) Done() <-chan struct{} {
	return m.doneChan
}

// shortTaskID returns shortened task ID for logging
func (m *Monitor) shortTaskID() string {
	if len(m.taskID) > 12 {
		return m.taskID[:12]
	}
	return m.taskID
}
