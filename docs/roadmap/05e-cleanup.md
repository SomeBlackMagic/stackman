# Этап 5д — Очистка ресурсов (`internal/swarm/cleanup.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 5а: DockerClient интерфейс
- [x] Этап 5б: GetStackState

## Что создаём
```
internal/swarm/
├── cleanup.go
└── cleanup_test.go
```

---

## TDD-план

### Цикл 1: RemoveExitedContainers удаляет завершённые контейнеры

**🔴 Тест:**
```go
// internal/swarm/cleanup_test.go
package swarm_test

import (
    "context"
    "testing"

    "github.com/docker/docker/api/types"
    "github.com/docker/docker/api/types/container"
    "github.com/docker/docker/api/types/filters"
    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestRemoveExitedContainers_RemovesExited(t *testing.T) {
    removed := make(map[string]bool)
    mock := &swarmint.MockDockerClient{
        ContainerListFn: func(_ context.Context, opts container.ListOptions) ([]types.Container, error) {
            // Проверяем что запрос с фильтрами по стеку
            return []types.Container{
                {ID: "c1", Status: "exited"},
                {ID: "c2", Status: "dead"},
            }, nil
        },
        ContainerRemoveFn: func(_ context.Context, id string, _ container.RemoveOptions) error {
            removed[id] = true
            return nil
        },
    }

    err := swarmint.RemoveExitedContainers(context.Background(), mock, "mystack")
    if err != nil {
        t.Fatal(err)
    }
    if !removed["c1"] || !removed["c2"] {
        t.Error("expected both exited containers to be removed")
    }
}
```

**🟢 Реализация `cleanup.go`:**
```go
// internal/swarm/cleanup.go
package swarm

import (
    "context"
    "fmt"

    "github.com/docker/docker/api/types/container"
    "github.com/docker/docker/api/types/filters"
)

// RemoveExitedContainers удаляет контейнеры стека в статусе exited или dead.
func RemoveExitedContainers(ctx context.Context, client DockerClient, stackName string) error {
    containers, err := client.ContainerList(ctx, container.ListOptions{
        All: true,
        Filters: filters.NewArgs(
            filters.Arg("label", "com.docker.stack.namespace="+stackName),
            filters.Arg("status", "exited"),
            filters.Arg("status", "dead"),
        ),
    })
    if err != nil {
        return fmt.Errorf("list exited containers: %w", err)
    }

    for _, c := range containers {
        if err := client.ContainerRemove(ctx, c.ID, container.RemoveOptions{
            Force:         true,
            RemoveVolumes: false,
        }); err != nil {
            return fmt.Errorf("remove container %s: %w", c.ID[:12], err)
        }
    }
    return nil
}
```

---

### Цикл 2: RemoveExitedContainers не трогает running контейнеры

**🔴 Тест:**
```go
func TestRemoveExitedContainers_KeepsRunning(t *testing.T) {
    removeCalled := 0
    mock := &swarmint.MockDockerClient{
        ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]types.Container, error) {
            // Возвращаем только running — Docker фильтрует по status=exited,dead
            return nil, nil
        },
        ContainerRemoveFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
            removeCalled++
            return nil
        },
    }
    swarmint.RemoveExitedContainers(context.Background(), mock, "mystack")
    if removeCalled != 0 {
        t.Error("running containers should not be removed")
    }
}
```

---

### Цикл 3: PruneOrphanedServices удаляет сервисы не из desired

**🔴 Тест:**
```go
func TestPruneOrphanedServices_RemovesOrphaned(t *testing.T) {
    removed := make(map[string]bool)
    mock := &swarmint.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarm.Service, error) {
            return []swarm.Service{
                {ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_web"}}},
                {ID: "svc2", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_legacy"}}},
            }, nil
        },
        ServiceRemoveFn: func(_ context.Context, id string) error {
            removed[id] = true
            return nil
        },
    }

    // desired содержит только "web" — "legacy" orphaned
    desired := map[string]bool{"web": true}
    err := swarmint.PruneOrphanedServices(context.Background(), mock, "mystack", desired)
    if err != nil {
        t.Fatal(err)
    }
    if removed["svc1"] {
        t.Error("web should not be removed (in desired)")
    }
    if !removed["svc2"] {
        t.Error("legacy should be removed (orphaned)")
    }
}
```

**🟢 Добавить в `cleanup.go`:**
```go
// PruneOrphanedServices удаляет сервисы стека которых нет в desired.
// desired — множество имён сервисов БЕЗ префикса стека.
func PruneOrphanedServices(ctx context.Context, client DockerClient, stackName string, desired map[string]bool) error {
    services, err := client.ServiceList(ctx, types.ServiceListOptions{
        Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
    })
    if err != nil {
        return fmt.Errorf("list services: %w", err)
    }

    prefix := stackName + "_"
    for _, svc := range services {
        name := strings.TrimPrefix(svc.Spec.Name, prefix)
        if !desired[name] {
            if err := client.ServiceRemove(ctx, svc.ID); err != nil {
                return fmt.Errorf("remove orphaned service %q: %w", svc.Spec.Name, err)
            }
        }
    }
    return nil
}
```

---

### Цикл 4: PruneOrphanedServices не трогает сервисы других стеков

**🔴 Тест:**
```go
func TestPruneOrphanedServices_DoesNotTouchOtherStacks(t *testing.T) {
    removeCalled := 0
    mock := &swarmint.MockDockerClient{
        ServiceListFn: func(_ context.Context, opts types.ServiceListOptions) ([]swarm.Service, error) {
            // Docker фильтрует по label — возвращаем только сервисы нашего стека
            return []swarm.Service{}, nil // наш стек пуст
        },
        ServiceRemoveFn: func(_ context.Context, _ string) error {
            removeCalled++
            return nil
        },
    }
    desired := map[string]bool{}
    swarmint.PruneOrphanedServices(context.Background(), mock, "mystack", desired)
    if removeCalled != 0 {
        t.Error("should not remove services when list is empty")
    }
}
```

---

### Цикл 5: PruneOrphanedNetworks

**🔴 Тест:**
```go
func TestPruneOrphanedNetworks_RemovesOrphaned(t *testing.T) {
    removed := make(map[string]bool)
    mock := &swarmint.MockDockerClient{
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
            return []network.Summary{
                {ID: "net1", Name: "mystack_frontend"},
                {ID: "net2", Name: "mystack_legacy-net"},
            }, nil
        },
        NetworkRemoveFn: func(_ context.Context, id string) error {
            removed[id] = true
            return nil
        },
    }
    desired := map[string]bool{"frontend": true}
    err := swarmint.PruneOrphanedNetworks(context.Background(), mock, "mystack", desired)
    if err != nil {
        t.Fatal(err)
    }
    if removed["net1"] {
        t.Error("frontend network should not be removed")
    }
    if !removed["net2"] {
        t.Error("legacy-net should be removed")
    }
}
```

**🟢 Добавить в `cleanup.go`:**
```go
func PruneOrphanedNetworks(ctx context.Context, client DockerClient, stackName string, desired map[string]bool) error {
    nets, err := client.NetworkList(ctx, network.ListOptions{
        Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
    })
    if err != nil {
        return fmt.Errorf("list networks: %w", err)
    }
    prefix := stackName + "_"
    for _, net := range nets {
        name := strings.TrimPrefix(net.Name, prefix)
        if !desired[name] {
            if err := client.NetworkRemove(ctx, net.ID); err != nil {
                return fmt.Errorf("remove orphaned network %q: %w", net.Name, err)
            }
        }
    }
    return nil
}
```

---

## Критерии завершения этапа

- [ ] `RemoveExitedContainers` удаляет контейнеры со статусом exited/dead
- [ ] `RemoveExitedContainers` не вызывает `ContainerRemove` когда список пуст
- [ ] `PruneOrphanedServices` удаляет сервисы не в desired
- [ ] `PruneOrphanedServices` не трогает сервисы из desired
- [ ] `PruneOrphanedNetworks` аналогично для сетей
- [ ] `go test ./internal/swarm/ -run TestRemoveExited,TestPrune` — зелёный

## Следующий этап
→ [Этап 5е: Оркестратор деплоя](./05f-stack-deployer.md)
