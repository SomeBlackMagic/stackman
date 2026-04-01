# Этап 5б — Состояние стека (`internal/swarm/state.go`, `gateway.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 5а: DockerClient интерфейс и Mock

## Что создаём
```
internal/swarm/
├── state.go
├── gateway.go
├── state_test.go
└── gateway_test.go
```

---

## TDD-план

### Цикл 1: StackState — пустой стек

**🔴 Тест:**
```go
// internal/swarm/state_test.go
package swarm_test

import (
    "context"
    "testing"

    "github.com/docker/docker/api/types"
    dockertypes "github.com/docker/docker/api/types"
    "github.com/docker/docker/api/types/network"
    "github.com/docker/docker/api/types/swarm"
    "github.com/docker/docker/api/types/volume"
    "github.com/docker/docker/api/types/filters"

    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestGetStackState_EmptyStack(t *testing.T) {
    mock := &swarmint.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
            return nil, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
            return nil, nil
        },
        VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
            return volume.ListResponse{}, nil
        },
    }

    state, err := swarmint.GetStackState(context.Background(), mock, "mystack")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(state.Services) != 0 {
        t.Errorf("expected 0 services, got %d", len(state.Services))
    }
}
```

**🟢 Реализация `state.go`:**
```go
// internal/swarm/state.go
package swarm

import (
    "context"
    "fmt"
    "strings"

    "github.com/docker/docker/api/types"
    "github.com/docker/docker/api/types/filters"
    "github.com/docker/docker/api/types/network"
    "github.com/docker/docker/api/types/swarm"
    "github.com/docker/docker/api/types/volume"
)

// StackState — текущее состояние всех ресурсов стека в Swarm.
// Ключи map — имена без префикса стека (например "web", не "mystack_web").
type StackState struct {
    Services map[string]swarm.Service
    Networks map[string]network.Summary
    Volumes  map[string]volume.Volume
}

// GetStackState запрашивает текущее состояние стека из Swarm.
// Фильтрация по label com.docker.stack.namespace=stackName.
// Если стек не существует — возвращает пустой StackState без ошибки.
func GetStackState(ctx context.Context, client DockerClient, stackName string) (*StackState, error) {
    state := &StackState{
        Services: make(map[string]swarm.Service),
        Networks: make(map[string]network.Summary),
        Volumes:  make(map[string]volume.Volume),
    }

    // Сервисы
    services, err := client.ServiceList(ctx, types.ServiceListOptions{
        Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
    })
    if err != nil {
        return nil, fmt.Errorf("list services: %w", err)
    }
    prefix := stackName + "_"
    for _, svc := range services {
        name := strings.TrimPrefix(svc.Spec.Name, prefix)
        state.Services[name] = svc
    }

    // Сети
    networks, err := client.NetworkList(ctx, network.ListOptions{
        Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
    })
    if err != nil {
        return nil, fmt.Errorf("list networks: %w", err)
    }
    for _, net := range networks {
        name := strings.TrimPrefix(net.Name, prefix)
        state.Networks[name] = net
    }

    // Тома
    vols, err := client.VolumeList(ctx, filters.NewArgs(
        filters.Arg("label", "com.docker.stack.namespace="+stackName),
    ))
    if err != nil {
        return nil, fmt.Errorf("list volumes: %w", err)
    }
    for _, vol := range vols.Volumes {
        name := strings.TrimPrefix(vol.Name, prefix)
        state.Volumes[name] = *vol
    }

    return state, nil
}
```

---

### Цикл 2: Фильтрация по namespace — чужие сервисы не попадают

**🔴 Тест:**
```go
func TestGetStackState_FiltersOtherStacks(t *testing.T) {
    myStackSvc := swarm.Service{
        ID: "svc1",
        Spec: swarm.ServiceSpec{
            Annotations: swarm.Annotations{
                Name: "mystack_web",
                Labels: map[string]string{
                    "com.docker.stack.namespace": "mystack",
                },
            },
        },
    }
    otherStackSvc := swarm.Service{
        ID: "svc2",
        Spec: swarm.ServiceSpec{
            Annotations: swarm.Annotations{
                Name: "otherstack_web",
                Labels: map[string]string{
                    "com.docker.stack.namespace": "otherstack",
                },
            },
        },
    }

    mock := &swarmint.MockDockerClient{
        ServiceListFn: func(_ context.Context, opts dockertypes.ServiceListOptions) ([]swarm.Service, error) {
            // Имитируем что Docker уже фильтрует по label
            f := opts.Filters
            if f.Contains("label") {
                for _, v := range f.Get("label") {
                    if strings.Contains(v, "mystack") {
                        return []swarm.Service{myStackSvc}, nil
                    }
                }
            }
            return []swarm.Service{myStackSvc, otherStackSvc}, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
            return nil, nil
        },
        VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
            return volume.ListResponse{}, nil
        },
    }

    state, err := swarmint.GetStackState(context.Background(), mock, "mystack")
    if err != nil {
        t.Fatal(err)
    }
    if _, ok := state.Services["web"]; !ok {
        t.Error("expected 'web' service in state")
    }
    if _, ok := state.Services["otherstack_web"]; ok {
        t.Error("other stack's service should not be in state")
    }
}
```

---

### Цикл 3: Имена без префикса стека

**🔴 Тест:**
```go
func TestGetStackState_ServiceNameStripsPrefix(t *testing.T) {
    mock := &swarmint.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
            return []swarm.Service{
                {Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_api"}}},
                {Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_db"}}},
            }, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
    }

    state, _ := swarmint.GetStackState(context.Background(), mock, "mystack")

    if _, ok := state.Services["api"]; !ok {
        t.Error("expected key 'api'")
    }
    if _, ok := state.Services["db"]; !ok {
        t.Error("expected key 'db'")
    }
    if _, ok := state.Services["mystack_api"]; ok {
        t.Error("key should not contain stack prefix")
    }
}
```

---

### Цикл 4: IsMultiNodeSwarm

**🔴 Тест:**
```go
// internal/swarm/gateway_test.go
package swarm_test

func TestIsMultiNodeSwarm_SingleNode(t *testing.T) {
    mock := &swarmint.MockDockerClient{
        NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
            return []swarm.Node{{ID: "node1"}}, nil
        },
    }
    multi, err := swarmint.IsMultiNodeSwarm(context.Background(), mock)
    if err != nil {
        t.Fatal(err)
    }
    if multi {
        t.Error("single node should not be multi-node")
    }
}

func TestIsMultiNodeSwarm_MultiNode(t *testing.T) {
    mock := &swarmint.MockDockerClient{
        NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
            return []swarm.Node{{ID: "node1"}, {ID: "node2"}}, nil
        },
    }
    multi, err := swarmint.IsMultiNodeSwarm(context.Background(), mock)
    if err != nil {
        t.Fatal(err)
    }
    if !multi {
        t.Error("two nodes should be multi-node")
    }
}
```

**🟢 Реализация `gateway.go`:**
```go
// internal/swarm/gateway.go
package swarm

import (
    "context"
    "errors"
    "fmt"
    "net"

    "github.com/docker/docker/api/types"
)

// ErrMultiNodeSwarm возвращается когда host-gateway недоступен в multi-node кластере.
var ErrMultiNodeSwarm = errors.New("host-gateway resolution is not supported in multi-node swarm")

func IsMultiNodeSwarm(ctx context.Context, client DockerClient) (bool, error) {
    nodes, err := client.NodeList(ctx, types.NodeListOptions{})
    if err != nil {
        return false, fmt.Errorf("list nodes: %w", err)
    }
    return len(nodes) > 1, nil
}

// DetectHostGatewayIP возвращает IP docker0 bridge.
// Возвращает ErrMultiNodeSwarm если кластер multi-node.
func DetectHostGatewayIP(ctx context.Context, client DockerClient) (string, error) {
    multi, err := IsMultiNodeSwarm(ctx, client)
    if err != nil {
        return "", err
    }
    if multi {
        return "", ErrMultiNodeSwarm
    }
    iface, err := net.InterfaceByName("docker0")
    if err != nil {
        return "", fmt.Errorf("find docker0 interface: %w", err)
    }
    addrs, err := iface.Addrs()
    if err != nil || len(addrs) == 0 {
        return "", fmt.Errorf("get docker0 addresses: %w", err)
    }
    ip, _, err := net.ParseCIDR(addrs[0].String())
    if err != nil {
        return "", err
    }
    return ip.String(), nil
}
```

### Цикл 5: DetectHostGatewayIP возвращает ошибку на multi-node

**🔴 Тест:**
```go
func TestDetectHostGatewayIP_MultiNodeError(t *testing.T) {
    mock := &swarmint.MockDockerClient{
        NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
            return []swarm.Node{{ID: "n1"}, {ID: "n2"}}, nil
        },
    }
    _, err := swarmint.DetectHostGatewayIP(context.Background(), mock)
    if !errors.Is(err, swarmint.ErrMultiNodeSwarm) {
        t.Errorf("expected ErrMultiNodeSwarm, got %v", err)
    }
}
```

---

## Критерии завершения этапа

- [ ] `GetStackState` для несуществующего стека → пустой `StackState`, нет ошибки
- [ ] Ключи в `state.Services` — без префикса стека
- [ ] Сервисы чужого стека не попадают в state
- [ ] `IsMultiNodeSwarm` — false для 1 ноды, true для 2+
- [ ] `DetectHostGatewayIP` → `ErrMultiNodeSwarm` для multi-node
- [ ] `go test ./internal/swarm/ -run TestGetStackState,TestIsMultiNode,TestDetectHostGateway` — зелёный

## Следующий этап
→ [Этап 5в: Сети и тома](./05c-networks-volumes.md)
