package health

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
)

// Monitor monitors a single task's lifecycle, health, and logs
// It automatically cleans up goroutines when task reaches terminal state
type Monitor struct {
	client DockerClient
	logger Logger

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

	// Configuration
	showLogs bool // whether to stream container logs

	// Channels for coordination
	eventChan        chan Event    // receives events for this task
	stopChan         chan struct{} // signals monitor to stop
	doneChan         chan struct{} // signals monitor has stopped
	containerReadyCh chan struct{} // closed when containerID is first set

	// Lifecycle management
	shutdownOnce  sync.Once
	containerOnce sync.Once // ensures containerReadyCh is closed exactly once
	stopped       bool

	// Thread safety
	mu sync.RWMutex
}

// NewMonitor creates a new task monitor
func NewMonitor(client DockerClient, taskID string, serviceID string, serviceName string) *Monitor {
	return NewMonitorWithLogsAndLogger(client, taskID, serviceID, serviceName, true, nil)
}

// NewMonitorWithLogs creates a new task monitor with optional log streaming.
func NewMonitorWithLogs(client DockerClient, taskID string, serviceID string, serviceName string, showLogs bool) *Monitor {
	return NewMonitorWithLogsAndLogger(client, taskID, serviceID, serviceName, showLogs, nil)
}

// NewMonitorWithLogsAndLogger creates a new task monitor with optional log streaming and custom logger.
func NewMonitorWithLogsAndLogger(client DockerClient, taskID string, serviceID string, serviceName string, showLogs bool, logger Logger) *Monitor {
	return &Monitor{
		client:           client,
		logger:           withDefaultLogger(logger),
		taskID:           taskID,
		serviceID:        serviceID,
		serviceName:      serviceName,
		showLogs:         showLogs,
		eventChan:        make(chan Event, 10),
		stopChan:         make(chan struct{}),
		doneChan:         make(chan struct{}),
		containerReadyCh: make(chan struct{}),
		healthStatus:     "unknown",
		lastSeen:         time.Now(),
	}
}

// Start begins monitoring the task
// This method blocks until task reaches terminal state or context is cancelled
func (m *Monitor) Start(ctx context.Context) error {
	defer close(m.doneChan)
	defer m.cleanup()

	m.logf("[TaskMonitor] Started monitoring task %s (service: %s)", m.shortTaskID(), m.serviceName)

	// Start goroutines for different monitoring aspects
	var wg sync.WaitGroup

	wg.Add(1)
	go m.runWorker(ctx, &wg, m.monitorHealth)

	if m.showLogs {
		wg.Add(1)
		go m.runWorker(ctx, &wg, m.runStreamLogs)
	}

	wg.Add(1)
	go m.runWorker(ctx, &wg, m.processEvents)

	// Wait for completion
	select {
	case <-ctx.Done():
		m.logf("[TaskMonitor] Context cancelled for task %s", m.shortTaskID())
		m.Stop()
	case <-m.stopChan:
		m.logf("[TaskMonitor] Stop signal received for task %s", m.shortTaskID())
	}

	// Wait for all goroutines to finish
	wg.Wait()

	m.logf("[TaskMonitor] Stopped monitoring task %s", m.shortTaskID())
	return nil
}

// Stop signals the monitor to stop
func (m *Monitor) Stop() {
	m.shutdownOnce.Do(func() {
		m.mu.Lock()
		m.stopped = true
		m.mu.Unlock()
		close(m.stopChan)
	})
}

// SendEvent sends an event to this task monitor
// Returns false if monitor is shutting down
func (m *Monitor) SendEvent(event Event) bool {
	m.mu.RLock()
	stopped := m.stopped
	m.mu.RUnlock()

	if stopped {
		return false
	}

	select {
	case m.eventChan <- event:
		return true
	case <-m.stopChan:
		return false
	default:
		// Channel full, log warning
		m.logf("[TaskMonitor] WARNING: Event channel full for task %s", m.shortTaskID())
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
				m.logf("[TaskMonitor] Task %s reached terminal state: %s",
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
		m.containerOnce.Do(func() { close(m.containerReadyCh) })
		m.logf("[TaskMonitor] Got container ID for task %s: %s", m.shortTaskID(), event.ContainerID[:12])
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
		m.logf("[TaskMonitor] 🆕 Task %s created%s", m.shortTaskID(), containerInfo)
	case EventTypeStarted:
		m.logf("[TaskMonitor] ▶️  Task %s started", m.shortTaskID())
	case EventTypeHealthy:
		m.logf("[TaskMonitor] 💚 Task %s is healthy (checks: %d)",
			m.shortTaskID(), m.healthChecks)
	case EventTypeUnhealthy:
		m.logf("[TaskMonitor] 💔 Task %s is unhealthy (failed: %d/%d)",
			m.shortTaskID(), m.failedChecks, m.healthChecks)
	case EventTypeFailed:
		m.logf("[TaskMonitor] ❌ Task %s failed: %s", m.shortTaskID(), event.Message)
	case EventTypeCompleted:
		m.logf("[TaskMonitor] 🏁 Task %s completed", m.shortTaskID())
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
	// If Docker client is not initialized, skip log streaming
	if m.client == nil {
		m.logf("[TaskLogs] Docker client is nil, skipping log streaming for task %s", m.shortTaskID())
		return
	}

	m.logf("[TaskLogs] Waiting for container ID for task %s...", m.shortTaskID())

	// Block until containerID is set by handleEvent, or until context/stop signal.
	// containerReadyCh is closed by containerOnce.Do in handleEvent — no polling needed.
	select {
	case <-ctx.Done():
		m.logf("[TaskLogs] Context done while waiting for container for task %s", m.shortTaskID())
		return
	case <-m.stopChan:
		m.logf("[TaskLogs] Stop signal while waiting for container for task %s", m.shortTaskID())
		return
	case <-m.containerReadyCh:
	}

	m.mu.RLock()
	containerID := m.containerID
	state := m.state
	m.mu.RUnlock()

	m.logf("[TaskLogs] Container %s for task %s is ready (state: %s)", containerID[:12], m.shortTaskID(), state)

	m.logf("[TaskLogs] About to start streaming logs for %s/%s (container: %s)", m.serviceName, m.shortTaskID(), containerID[:12])

	// Start streaming logs - get ALL logs, not just from now
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
		// Don't use Since - get all logs from container start
	}

	logReader, err := m.client.ContainerLogs(ctx, containerID, options)
	if err != nil {
		m.logf("[TaskLogs] Failed to stream logs for task %s: %v", m.shortTaskID(), err)
		return
	}
	defer logReader.Close()

	m.logf("[TaskLogs] Successfully opened log stream for %s/%s, starting to read...", m.serviceName, m.shortTaskID())

	// Docker multiplexes stdout/stderr in a special format
	// Header: [8]byte{STREAM_TYPE, 0, 0, 0, SIZE1, SIZE2, SIZE3, SIZE4}
	// STREAM_TYPE: 0=stdin, 1=stdout, 2=stderr
	// SIZE: uint32 big-endian, size of frame

	buf := make([]byte, 8192)
	logsReceived := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		default:
		}

		// Read header (8 bytes)
		header := make([]byte, 8)
		n, err := logReader.Read(header)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
				m.logf("[TaskLogs] Log stream error for %s: %v", m.shortTaskID(), err)
			}
			return
		}
		if n != 8 {
			if n > 0 {
				m.logf("[TaskLogs] Unexpected header size for %s: %d bytes", m.shortTaskID(), n)
			}
			continue
		}

		// Parse frame size from header
		frameSize := uint32(header[4])<<24 | uint32(header[5])<<16 | uint32(header[6])<<8 | uint32(header[7])
		if frameSize == 0 {
			continue
		}

		// Read frame data
		if frameSize > uint32(len(buf)) {
			buf = make([]byte, frameSize)
		}

		n, err = logReader.Read(buf[:frameSize])
		if err != nil {
			return
		}

		// Output log line with service/task prefix
		streamPrefix := "📄"
		if header[0] == 1 {
			streamPrefix = "📘" // stdout
		} else if header[0] == 2 {
			streamPrefix = "📕" // stderr
		}

		logLine := string(buf[:n])
		logsReceived++

		// Use fmt.Printf to output directly to stdout (not via logger)
		fmt.Printf("%s [%s/%s] %s", streamPrefix, m.serviceName, m.shortTaskID(), logLine)
		if len(logLine) > 0 && logLine[len(logLine)-1] != '\n' {
			fmt.Println() // Add newline if not present
		}

		// Log every 10 lines to show we're receiving data
		if logsReceived%10 == 1 {
			//TODO debug log
			//m.logf("[TaskLogs] Received %d log lines from %s/%s", logsReceived, m.serviceName, m.shortTaskID())
		}
	}
}

// cleanup performs cleanup when monitor stops.
// eventChan is intentionally not closed here: all goroutines have already exited
// via stopChan by the time cleanup runs (after wg.Wait()). Closing eventChan would
// risk a panic if SendEvent is called concurrently and its select picks the closed
// channel branch over the already-closed stopChan branch.
func (m *Monitor) cleanup() {
	m.logf("[TaskMonitor] Cleaning up monitor for task %s", m.shortTaskID())
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
	return m.healthStatus == container.Healthy
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

// runWorker executes fn with ctx, decrementing wg when fn returns.
// Used to start named goroutines from Start() without anonymous closures.
func (m *Monitor) runWorker(ctx context.Context, wg *sync.WaitGroup, fn func(context.Context)) {
	defer wg.Done()
	defer m.recoverWorkerPanic()
	fn(ctx)
}

// runStreamLogs wraps streamLogs with lifecycle logging.
func (m *Monitor) runStreamLogs(ctx context.Context) {
	m.logf("[TaskMonitor] Log streaming goroutine started for task %s", m.shortTaskID())
	m.streamLogs(ctx)
	m.logf("[TaskMonitor] Log streaming goroutine ended for task %s", m.shortTaskID())
}

// shortTaskID returns shortened task ID for logging
func (m *Monitor) shortTaskID() string {
	if len(m.taskID) > 12 {
		return m.taskID[:12]
	}
	return m.taskID
}

func (m *Monitor) logf(format string, args ...any) {
	m.logger.Printf(format, args...)
}

func (m *Monitor) recoverWorkerPanic() {
	if recovered := recover(); recovered != nil {
		m.logf("[TaskMonitor] panic recovered for task %s: %v\n%s", m.shortTaskID(), recovered, string(debug.Stack()))
		m.Stop()
	}
}
