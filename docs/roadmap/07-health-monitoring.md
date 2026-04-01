# Этап 7 — Мониторинг здоровья (`internal/health/`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 5а: DockerClient интерфейс

## Что создаём
```
internal/health/
├── events.go
├── watcher.go
├── monitor.go
├── service_update_monitor.go
├── events_test.go
├── monitor_test.go
└── service_update_monitor_test.go
```

---

## TDD-план

### Цикл 1: Типы событий

**🔴 Тест:**
```go
// internal/health/events_test.go
package health_test

import (
    "testing"
    "github.com/SomeBlackMagic/stackman/internal/health"
)

func TestEventTypes_Constants(t *testing.T) {
    // Проверяем что все константы определены и уникальны
    types := []health.EventType{
        health.EventTypeCreated,
        health.EventTypeStarted,
        health.EventTypeRunning,
        health.EventTypeHealthy,
        health.EventTypeUnhealthy,
        health.EventTypeFailed,
        health.EventTypeShutdown,
    }
    seen := make(map[health.EventType]bool)
    for _, t := range types {
        if seen[t] {
            t.Errorf("duplicate event type: %q", t)
        }
        seen[t] = true
    }
}

func TestEvent_IsTerminal(t *testing.T) {
    tests := []struct {
        eventType health.EventType
        terminal  bool
    }{
        {health.EventTypeHealthy, true},
        {health.EventTypeFailed, true},
        {health.EventTypeShutdown, true},
        {health.EventTypeRunning, false},
        {health.EventTypeStarted, false},
        {health.EventTypeUnhealthy, false},
    }
    for _, tt := range tests {
        e := health.Event{Type: tt.eventType}
        if e.IsTerminal() != tt.terminal {
            t.Errorf("EventType %q: IsTerminal()=%v, want %v", tt.eventType, e.IsTerminal(), tt.terminal)
        }
    }
}

func TestEvent_IsFailure(t *testing.T) {
    if !(health.Event{Type: health.EventTypeFailed}).IsFailure() {
        t.Error("Failed should be failure")
    }
    if !(health.Event{Type: health.EventTypeUnhealthy}).IsFailure() {
        t.Error("Unhealthy should be failure")
    }
    if (health.Event{Type: health.EventTypeHealthy}).IsFailure() {
        t.Error("Healthy should not be failure")
    }
}
```

**🟢 Реализация `events.go`:**
```go
// internal/health/events.go
package health

import "time"

type EventType string

const (
    EventTypeCreated   EventType = "task_created"
    EventTypeStarted   EventType = "task_started"
    EventTypeRunning   EventType = "task_running"
    EventTypeHealthy   EventType = "task_healthy"
    EventTypeUnhealthy EventType = "task_unhealthy"
    EventTypeFailed    EventType = "task_failed"
    EventTypeShutdown  EventType = "task_shutdown"
)

type Event struct {
    Type        EventType
    TaskID      string
    ServiceID   string
    ServiceName string
    ContainerID string
    NodeID      string
    Timestamp   time.Time
    Message     string
    Error       string
}

// IsTerminal возвращает true если событие означает завершение жизненного цикла задачи.
func (e Event) IsTerminal() bool {
    switch e.Type {
    case EventTypeHealthy, EventTypeFailed, EventTypeShutdown:
        return true
    }
    return false
}

// IsFailure возвращает true если событие означает ошибку.
func (e Event) IsFailure() bool {
    switch e.Type {
    case EventTypeFailed, EventTypeUnhealthy:
        return true
    }
    return false
}
```

---

### Цикл 2: ServiceUpdateMonitor — ожидание healthy (без healthcheck)

**🔴 Тест:**
```go
// internal/health/service_update_monitor_test.go
package health_test

import (
    "context"
    "testing"
    "time"

    "github.com/docker/docker/api/types"
    "github.com/docker/docker/api/types/swarm"
    "github.com/docker/docker/api/types/filters"

    healthmod "github.com/SomeBlackMagic/stackman/internal/health"
    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestServiceUpdateMonitor_HealthyWhenAllRunning(t *testing.T) {
    replicas := uint64(2)
    callCount := 0

    mock := &swarmint.MockDockerClient{
        TaskListFn: func(_ context.Context, opts types.TaskListOptions) ([]swarm.Task, error) {
            callCount++
            return []swarm.Task{
                {
                    ID:           "task1",
                    DesiredState: swarm.TaskStateRunning,
                    Status: swarm.TaskStatus{
                        State: swarm.TaskStateRunning,
                    },
                },
                {
                    ID:           "task2",
                    DesiredState: swarm.TaskStateRunning,
                    Status: swarm.TaskStatus{
                        State: swarm.TaskStateRunning,
                    },
                },
            }, nil
        },
        ServiceInspectWithRawFn: func(_ context.Context, _ string, _ types.ServiceInspectOptions) (swarm.Service, []byte, error) {
            return swarm.Service{
                Spec: swarm.ServiceSpec{
                    Mode: swarm.ServiceMode{
                        Replicated: &swarm.ReplicatedService{Replicas: &replicas},
                    },
                },
            }, nil, nil
        },
    }

    mon := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    err := mon.WaitHealthy(ctx, 5*time.Second)
    if err != nil {
        t.Fatalf("expected healthy, got error: %v", err)
    }
}
```

**🟢 Реализация `service_update_monitor.go`:**
```go
// internal/health/service_update_monitor.go
package health

import (
    "context"
    "fmt"
    "time"

    dockertypes "github.com/docker/docker/api/types"
    "github.com/docker/docker/api/types/filters"
    "github.com/docker/docker/api/types/swarm"

    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

type ServiceUpdateMonitor struct {
    client      swarmint.DockerClient
    serviceID   string
    serviceName string
}

func NewServiceUpdateMonitor(client swarmint.DockerClient, serviceID, serviceName string) *ServiceUpdateMonitor {
    return &ServiceUpdateMonitor{
        client:      client,
        serviceID:   serviceID,
        serviceName: serviceName,
    }
}

// WaitHealthy ждёт пока все реплики сервиса станут healthy (или просто running если нет healthcheck).
// Возвращает ошибку при таймауте или при обнаружении failed задач.
func (m *ServiceUpdateMonitor) WaitHealthy(ctx context.Context, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return fmt.Errorf("service %q: timeout waiting for healthy state", m.serviceName)
        case <-ticker.C:
            healthy, err := m.checkHealth(ctx)
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
    // Получаем задачи сервиса
    tasks, err := m.client.TaskList(ctx, dockertypes.TaskListOptions{
        Filters: filters.NewArgs(
            filters.Arg("service", m.serviceID),
            filters.Arg("desired-state", "running"),
        ),
    })
    if err != nil {
        return false, fmt.Errorf("list tasks: %w", err)
    }

    // Получаем желаемое число реплик
    svc, _, err := m.client.ServiceInspectWithRaw(ctx, m.serviceID, dockertypes.ServiceInspectOptions{})
    if err != nil {
        return false, fmt.Errorf("inspect service: %w", err)
    }

    desired := uint64(1)
    if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
        desired = *svc.Spec.Mode.Replicated.Replicas
    }

    // Считаем running задачи
    running := uint64(0)
    for _, task := range tasks {
        switch task.Status.State {
        case swarm.TaskStateFailed, swarm.TaskStateRejected:
            return false, fmt.Errorf("task %s failed: %s", task.ID[:12], task.Status.Err)
        case swarm.TaskStateRunning:
            // Если есть healthcheck — проверяем health статус
            if task.Status.ContainerStatus != nil &&
                task.Status.ContainerStatus.ContainerID != "" {
                // Здесь можно добавить проверку container health через ContainerInspect
                // Для MVP: считаем running как healthy
            }
            running++
        }
    }

    return running >= desired, nil
}
```

---

### Цикл 3: ServiceUpdateMonitor — возвращает ошибку при таймауте

**🔴 Тест:**
```go
func TestServiceUpdateMonitor_Timeout(t *testing.T) {
    replicas := uint64(2)
    mock := &swarmint.MockDockerClient{
        TaskListFn: func(_ context.Context, _ types.TaskListOptions) ([]swarm.Task, error) {
            // Всегда возвращаем только 1 running задачу из 2
            return []swarm.Task{
                {Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
            }, nil
        },
        ServiceInspectWithRawFn: func(_ context.Context, _ string, _ types.ServiceInspectOptions) (swarm.Service, []byte, error) {
            return swarm.Service{
                Spec: swarm.ServiceSpec{
                    Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
                },
            }, nil, nil
        },
    }

    mon := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
    err := mon.WaitHealthy(context.Background(), 100*time.Millisecond)
    if err == nil {
        t.Fatal("expected timeout error")
    }
    if !strings.Contains(err.Error(), "timeout") {
        t.Errorf("expected timeout message, got: %v", err)
    }
}
```

---

### Цикл 4: ServiceUpdateMonitor — ошибка при failed задаче

**🔴 Тест:**
```go
func TestServiceUpdateMonitor_FailedTask(t *testing.T) {
    replicas := uint64(1)
    mock := &swarmint.MockDockerClient{
        TaskListFn: func(_ context.Context, _ types.TaskListOptions) ([]swarm.Task, error) {
            return []swarm.Task{
                {
                    ID: "task-abc",
                    Status: swarm.TaskStatus{
                        State: swarm.TaskStateFailed,
                        Err:   "container exited with code 1",
                    },
                },
            }, nil
        },
        ServiceInspectWithRawFn: func(_ context.Context, _ string, _ types.ServiceInspectOptions) (swarm.Service, []byte, error) {
            return swarm.Service{
                Spec: swarm.ServiceSpec{
                    Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
                },
            }, nil, nil
        },
    }

    mon := healthmod.NewServiceUpdateMonitor(mock, "svc-id", "mystack_web")
    err := mon.WaitHealthy(context.Background(), 5*time.Second)
    if err == nil {
        t.Fatal("expected error for failed task")
    }
    if !strings.Contains(err.Error(), "failed") {
        t.Errorf("error should mention task failure, got: %v", err)
    }
}
```

---

### Цикл 5: EventWatcher конвертирует Docker события

**🔴 Тест:**
```go
// internal/health/monitor_test.go
package health_test

func TestEventWatcher_ConvertsDieToFailed(t *testing.T) {
    dockerEvents := make(chan dockerevents.Message, 1)
    errCh := make(chan error, 1)

    mock := &swarmint.MockDockerClient{
        EventsFn: func(_ context.Context, _ dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
            return dockerEvents, errCh
        },
        TaskListFn: func(_ context.Context, _ types.TaskListOptions) ([]swarm.Task, error) {
            return []swarm.Task{{
                ID: "task1",
                Status: swarm.TaskStatus{State: swarm.TaskStateFailed},
            }}, nil
        },
    }

    watcher := healthmod.NewWatcher(mock, "mystack")
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    events := watcher.Subscribe()
    go watcher.Start(ctx)

    // Отправляем docker событие "die"
    dockerEvents <- dockerevents.Message{
        Type:   "container",
        Action: "die",
        Actor: dockerevents.Actor{
            Attributes: map[string]string{
                "com.docker.stack.namespace": "mystack",
                "com.docker.swarm.task.id":  "task1",
            },
        },
    }

    select {
    case event := <-events:
        if event.Type != healthmod.EventTypeFailed {
            t.Errorf("expected EventTypeFailed, got %v", event.Type)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("timeout waiting for event")
    }
}
```

**🟢 Минимальная реализация `watcher.go`:**
```go
// internal/health/watcher.go
package health

import (
    "context"
    "strings"

    dockerevents "github.com/docker/docker/api/types/events"
    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

type Watcher struct {
    client      swarmint.DockerClient
    stackName   string
    subscribers []chan Event
}

func NewWatcher(client swarmint.DockerClient, stackName string) *Watcher {
    return &Watcher{client: client, stackName: stackName}
}

func (w *Watcher) Subscribe() <-chan Event {
    ch := make(chan Event, 64)
    w.subscribers = append(w.subscribers, ch)
    return ch
}

func (w *Watcher) Start(ctx context.Context) error {
    events, errs := w.client.Events(ctx, dockerevents.ListOptions{})
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case err := <-errs:
            return err
        case msg := <-events:
            if msg.Actor.Attributes["com.docker.stack.namespace"] != w.stackName {
                continue
            }
            event := w.convertEvent(msg)
            if event == nil {
                continue
            }
            for _, sub := range w.subscribers {
                select {
                case sub <- *event:
                default:
                }
            }
        }
    }
}

func (w *Watcher) convertEvent(msg dockerevents.Message) *Event {
    e := &Event{}
    switch msg.Action {
    case "die", "kill":
        e.Type = EventTypeFailed
    case "start":
        e.Type = EventTypeStarted
    case "health_status: healthy":
        e.Type = EventTypeHealthy
    case "health_status: unhealthy":
        e.Type = EventTypeUnhealthy
    default:
        if strings.HasPrefix(msg.Action, "health_status") {
            e.Type = EventTypeUnhealthy
        } else {
            return nil // неизвестное событие
        }
    }
    e.TaskID = msg.Actor.Attributes["com.docker.swarm.task.id"]
    e.ServiceID = msg.Actor.Attributes["com.docker.swarm.service.id"]
    return e
}
```

---

## Критерии завершения этапа

- [ ] Все константы `EventType` определены и уникальны
- [ ] `IsTerminal()` — true для Healthy/Failed/Shutdown
- [ ] `IsFailure()` — true для Failed/Unhealthy
- [ ] `WaitHealthy` возвращает `nil` когда все реплики running
- [ ] `WaitHealthy` возвращает ошибку с "timeout" при таймауте
- [ ] `WaitHealthy` возвращает ошибку с "failed" при `TaskStateFailed`
- [ ] `Watcher` конвертирует Docker событие "die" → `EventTypeFailed`
- [ ] `go test ./internal/health/...` — зелёный

## Следующий этап
→ [Этап 8: Планировщик](./08-planner.md)
