# Этап 5а — Docker Client Abstraction (`internal/swarm/interface.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 0: скелет проекта

## Что создаём
```
internal/swarm/
├── interface.go          # DockerClient интерфейс
├── mock_docker_client.go # Mock для тестов
└── interface_test.go
```

---

## Принцип

> Никакой код вне пакета `internal/swarm/` не импортирует Docker SDK напрямую.
> Весь доступ к Docker — только через интерфейс `DockerClient`.
> Это единственная точка замены для тестирования.

---

## TDD-план

### Цикл 1: Интерфейс компилируется

**🔴 Тест:**
```go
// internal/swarm/interface_test.go
package swarm_test

import (
    "testing"
    "github.com/SomeBlackMagic/stackman/internal/swarm"
)

// Проверяем что DockerClient — корректный интерфейс.
// Если он не компилируется — тест не соберётся.
func TestDockerClientInterface(t *testing.T) {
    var _ swarm.DockerClient = (*swarm.MockDockerClient)(nil)
}
```

**🟢 Реализация `interface.go`:**
```go
// internal/swarm/interface.go
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

// DockerClient — абстракция над Docker API.
// Все методы соответствуют методам client.Client из Docker SDK.
type DockerClient interface {
    // Сервисы
    ServiceList(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error)
    ServiceCreate(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (types.ServiceCreateResponse, error)
    ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (types.ServiceUpdateResponse, error)
    ServiceRemove(ctx context.Context, serviceID string) error
    ServiceInspectWithRaw(ctx context.Context, serviceID string, opts types.ServiceInspectOptions) (swarm.Service, []byte, error)

    // Задачи
    TaskList(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error)

    // Контейнеры
    ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
    ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
    ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
    ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)

    // Сети
    NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
    NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
    NetworkRemove(ctx context.Context, networkID string) error

    // Тома
    VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
    VolumeList(ctx context.Context, filter filters.Args) (volume.ListResponse, error)
    VolumeRemove(ctx context.Context, volumeID string, force bool) error

    // События
    Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error)

    // Ноды
    NodeList(ctx context.Context, options types.NodeListOptions) ([]swarm.Node, error)

    // Образы
    ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)

    Close() error
}
```

---

### Цикл 2: Mock реализует интерфейс

**🟢 Реализация `mock_docker_client.go`:**
```go
// internal/swarm/mock_docker_client.go
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

// MockDockerClient — тестовая реализация DockerClient.
// Каждый метод делегирует соответствующему Fn-полю.
// Если поле не установлено — паникует с понятным сообщением.
type MockDockerClient struct {
    ServiceListFn    func(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error)
    ServiceCreateFn  func(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (types.ServiceCreateResponse, error)
    ServiceUpdateFn  func(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (types.ServiceUpdateResponse, error)
    ServiceRemoveFn  func(ctx context.Context, serviceID string) error
    ServiceInspectWithRawFn func(ctx context.Context, serviceID string, opts types.ServiceInspectOptions) (swarm.Service, []byte, error)

    TaskListFn func(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error)

    ContainerListFn    func(ctx context.Context, options container.ListOptions) ([]types.Container, error)
    ContainerInspectFn func(ctx context.Context, containerID string) (types.ContainerJSON, error)
    ContainerRemoveFn  func(ctx context.Context, containerID string, options container.RemoveOptions) error
    ContainerLogsFn    func(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)

    NetworkCreateFn func(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
    NetworkListFn   func(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
    NetworkRemoveFn func(ctx context.Context, networkID string) error

    VolumeCreateFn func(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
    VolumeListFn   func(ctx context.Context, filter filters.Args) (volume.ListResponse, error)
    VolumeRemoveFn func(ctx context.Context, volumeID string, force bool) error

    EventsFn   func(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error)
    NodeListFn func(ctx context.Context, options types.NodeListOptions) ([]swarm.Node, error)
    ImagePullFn func(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
    CloseFn     func() error
}

// Compile-time check
var _ DockerClient = (*MockDockerClient)(nil)

func mustSet(method string) {
    panic(fmt.Sprintf("MockDockerClient.%s is not set", method))
}

func (m *MockDockerClient) ServiceList(ctx context.Context, opts types.ServiceListOptions) ([]swarm.Service, error) {
    if m.ServiceListFn == nil { mustSet("ServiceList") }
    return m.ServiceListFn(ctx, opts)
}

func (m *MockDockerClient) ServiceCreate(ctx context.Context, svc swarm.ServiceSpec, opts types.ServiceCreateOptions) (types.ServiceCreateResponse, error) {
    if m.ServiceCreateFn == nil { mustSet("ServiceCreate") }
    return m.ServiceCreateFn(ctx, svc, opts)
}

func (m *MockDockerClient) ServiceUpdate(ctx context.Context, id string, v swarm.Version, svc swarm.ServiceSpec, opts types.ServiceUpdateOptions) (types.ServiceUpdateResponse, error) {
    if m.ServiceUpdateFn == nil { mustSet("ServiceUpdate") }
    return m.ServiceUpdateFn(ctx, id, v, svc, opts)
}

func (m *MockDockerClient) ServiceRemove(ctx context.Context, id string) error {
    if m.ServiceRemoveFn == nil { mustSet("ServiceRemove") }
    return m.ServiceRemoveFn(ctx, id)
}

func (m *MockDockerClient) ServiceInspectWithRaw(ctx context.Context, id string, opts types.ServiceInspectOptions) (swarm.Service, []byte, error) {
    if m.ServiceInspectWithRawFn == nil { mustSet("ServiceInspectWithRaw") }
    return m.ServiceInspectWithRawFn(ctx, id, opts)
}

func (m *MockDockerClient) TaskList(ctx context.Context, opts types.TaskListOptions) ([]swarm.Task, error) {
    if m.TaskListFn == nil { mustSet("TaskList") }
    return m.TaskListFn(ctx, opts)
}

func (m *MockDockerClient) ContainerList(ctx context.Context, opts container.ListOptions) ([]types.Container, error) {
    if m.ContainerListFn == nil { mustSet("ContainerList") }
    return m.ContainerListFn(ctx, opts)
}

func (m *MockDockerClient) ContainerInspect(ctx context.Context, id string) (types.ContainerJSON, error) {
    if m.ContainerInspectFn == nil { mustSet("ContainerInspect") }
    return m.ContainerInspectFn(ctx, id)
}

func (m *MockDockerClient) ContainerRemove(ctx context.Context, id string, opts container.RemoveOptions) error {
    if m.ContainerRemoveFn == nil { mustSet("ContainerRemove") }
    return m.ContainerRemoveFn(ctx, id, opts)
}

func (m *MockDockerClient) ContainerLogs(ctx context.Context, c string, opts container.LogsOptions) (io.ReadCloser, error) {
    if m.ContainerLogsFn == nil { mustSet("ContainerLogs") }
    return m.ContainerLogsFn(ctx, c, opts)
}

func (m *MockDockerClient) NetworkCreate(ctx context.Context, name string, opts network.CreateOptions) (network.CreateResponse, error) {
    if m.NetworkCreateFn == nil { mustSet("NetworkCreate") }
    return m.NetworkCreateFn(ctx, name, opts)
}

func (m *MockDockerClient) NetworkList(ctx context.Context, opts network.ListOptions) ([]network.Summary, error) {
    if m.NetworkListFn == nil { mustSet("NetworkList") }
    return m.NetworkListFn(ctx, opts)
}

func (m *MockDockerClient) NetworkRemove(ctx context.Context, id string) error {
    if m.NetworkRemoveFn == nil { mustSet("NetworkRemove") }
    return m.NetworkRemoveFn(ctx, id)
}

func (m *MockDockerClient) VolumeCreate(ctx context.Context, opts volume.CreateOptions) (volume.Volume, error) {
    if m.VolumeCreateFn == nil { mustSet("VolumeCreate") }
    return m.VolumeCreateFn(ctx, opts)
}

func (m *MockDockerClient) VolumeList(ctx context.Context, f filters.Args) (volume.ListResponse, error) {
    if m.VolumeListFn == nil { mustSet("VolumeList") }
    return m.VolumeListFn(ctx, f)
}

func (m *MockDockerClient) VolumeRemove(ctx context.Context, id string, force bool) error {
    if m.VolumeRemoveFn == nil { mustSet("VolumeRemove") }
    return m.VolumeRemoveFn(ctx, id, force)
}

func (m *MockDockerClient) Events(ctx context.Context, opts events.ListOptions) (<-chan events.Message, <-chan error) {
    if m.EventsFn == nil { mustSet("Events") }
    return m.EventsFn(ctx, opts)
}

func (m *MockDockerClient) NodeList(ctx context.Context, opts types.NodeListOptions) ([]swarm.Node, error) {
    if m.NodeListFn == nil { mustSet("NodeList") }
    return m.NodeListFn(ctx, opts)
}

func (m *MockDockerClient) ImagePull(ctx context.Context, ref string, opts image.PullOptions) (io.ReadCloser, error) {
    if m.ImagePullFn == nil { mustSet("ImagePull") }
    return m.ImagePullFn(ctx, ref, opts)
}

func (m *MockDockerClient) Close() error {
    if m.CloseFn != nil {
        return m.CloseFn()
    }
    return nil
}
```

---

### Цикл 3: Mock паникует при неустановленном Fn

**🔴 Тест:**
```go
func TestMockDockerClient_PanicsWhenFnNotSet(t *testing.T) {
    mock := &swarm.MockDockerClient{}
    defer func() {
        r := recover()
        if r == nil {
            t.Error("expected panic")
        }
        msg := fmt.Sprintf("%v", r)
        if !strings.Contains(msg, "ServiceList") {
            t.Errorf("panic message should mention method name, got: %s", msg)
        }
    }()
    mock.ServiceList(context.Background(), types.ServiceListOptions{})
}
```

Тест пройдёт — `mustSet` уже паникует.

---

## Вспомогательная функция для тестов

Добавить в `mock_docker_client.go` удобный конструктор для типичных сценариев:

```go
// NewEmptyStackMock возвращает mock настроенный для пустого стека.
// Используется в тестах где стек ещё не существует.
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
```

---

## Критерии завершения этапа

- [ ] `var _ DockerClient = (*MockDockerClient)(nil)` компилируется
- [ ] Все методы интерфейса реализованы в Mock
- [ ] Вызов метода без Fn → паника с именем метода
- [ ] `Close()` без CloseFn → `nil` (не паникует)
- [ ] `go test ./internal/swarm/ -run TestDockerClient,TestMock` — зелёный

## Следующий этап
→ [Этап 5б: Состояние стека](./05b-stack-state.md)
