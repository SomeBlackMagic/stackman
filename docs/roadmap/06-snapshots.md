# Этап 6 — Снимки состояния (`internal/snapshot/`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 5е: StackDeployer.GetState

## Что создаём
```
internal/snapshot/
├── snapshot.go
└── snapshot_test.go
```

---

## TDD-план

### Цикл 1: TakeSnapshot для несуществующего стека

**🔴 Тест:**
```go
// internal/snapshot/snapshot_test.go
package snapshot_test

import (
    "context"
    "testing"

    "github.com/docker/docker/api/types"
    "github.com/docker/docker/api/types/filters"
    "github.com/docker/docker/api/types/network"
    "github.com/docker/docker/api/types/volume"
    "github.com/docker/docker/api/types/swarm"

    snapmod "github.com/SomeBlackMagic/stackman/internal/snapshot"
    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func emptyMock() *swarmint.MockDockerClient {
    return &swarmint.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarm.Service, error) {
            return nil, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
            return nil, nil
        },
        VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
            return volume.ListResponse{}, nil
        },
    }
}

func TestTakeSnapshot_EmptyStack(t *testing.T) {
    deployer := swarmint.NewStackDeployer(emptyMock(), "mystack")
    snap, err := snapmod.TakeSnapshot(context.Background(), deployer)
    if err != nil {
        t.Fatal(err)
    }
    if !snap.IsFirstDeploy {
        t.Error("empty stack should have IsFirstDeploy=true")
    }
    if len(snap.Services) != 0 {
        t.Errorf("expected 0 services, got %d", len(snap.Services))
    }
}
```

**🟢 Реализация `snapshot.go`:**
```go
// internal/snapshot/snapshot.go
package snapshot

import (
    "context"
    "fmt"
    "time"

    "github.com/docker/docker/api/types/network"
    "github.com/docker/docker/api/types/swarm"
    "github.com/docker/docker/api/types/volume"

    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

// StackSnapshot — состояние стека, захваченное перед деплоем.
type StackSnapshot struct {
    StackName     string
    TakenAt       time.Time
    IsFirstDeploy bool

    // Ключи — имена без префикса стека
    Services map[string]swarm.Service
    Networks map[string]network.Summary
    Volumes  map[string]volume.Volume
}

// TakeSnapshot захватывает текущее состояние стека.
// Если стек не существует (0 сервисов) — IsFirstDeploy=true.
func TakeSnapshot(ctx context.Context, deployer *swarmint.StackDeployer) (*StackSnapshot, error) {
    state, err := deployer.GetState(ctx)
    if err != nil {
        return nil, fmt.Errorf("get stack state: %w", err)
    }

    snap := &StackSnapshot{
        TakenAt:  time.Now().UTC(),
        Services: state.Services,
        Networks: state.Networks,
        Volumes:  state.Volumes,
    }
    snap.IsFirstDeploy = len(state.Services) == 0

    return snap, nil
}
```

---

### Цикл 2: TakeSnapshot захватывает существующие сервисы

**🔴 Тест:**
```go
func TestTakeSnapshot_CapturesServices(t *testing.T) {
    mock := &swarmint.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarm.Service, error) {
            return []swarm.Service{
                {ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_web"}}},
            }, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
    }

    deployer := swarmint.NewStackDeployer(mock, "mystack")
    snap, err := snapmod.TakeSnapshot(context.Background(), deployer)
    if err != nil {
        t.Fatal(err)
    }
    if snap.IsFirstDeploy {
        t.Error("should not be first deploy when services exist")
    }
    if _, ok := snap.Services["web"]; !ok {
        t.Error("expected 'web' in snapshot services")
    }
}
```

---

### Цикл 3: Rollback при IsFirstDeploy удаляет созданные сервисы

**🔴 Тест:**
```go
func TestRollback_FirstDeploy_RemovesServices(t *testing.T) {
    removed := make(map[string]bool)
    mock := &swarmint.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarm.Service, error) {
            // После деплоя появились сервисы
            return []swarm.Service{
                {ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_web"}}},
                {ID: "svc2", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_worker"}}},
            }, nil
        },
        ServiceRemoveFn: func(_ context.Context, id string) error {
            removed[id] = true
            return nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
    }

    snap := &snapmod.StackSnapshot{
        IsFirstDeploy: true,
        Services:      map[string]swarm.Service{},
        Networks:      map[string]network.Summary{},
        Volumes:       map[string]volume.Volume{},
    }

    err := snap.Rollback(context.Background(), mock)
    if err != nil {
        t.Fatal(err)
    }
    if !removed["svc1"] || !removed["svc2"] {
        t.Error("all services should be removed on first-deploy rollback")
    }
}
```

**🟢 Добавить метод `Rollback` в `snapshot.go`:**
```go
// Rollback восстанавливает состояние стека по снимку.
//   - IsFirstDeploy=true: удаляет все сервисы созданные в ходе деплоя
//   - IsFirstDeploy=false: обновляет сервисы до спецификации из снимка;
//     удаляет сервисы появившиеся после снимка
func (s *StackSnapshot) Rollback(ctx context.Context, client swarmint.DockerClient) error {
    // Получаем текущий список сервисов
    stackName := s.extractStackName()
    current, err := swarmint.GetStackState(ctx, client, stackName) // нам нужен stackName
    if err != nil {
        return fmt.Errorf("get current state for rollback: %w", err)
    }

    if s.IsFirstDeploy {
        // Удаляем всё что было создано
        for _, svc := range current.Services {
            svcFull := current.Services[svc] // нам нужен ID
        }
        // ... реализация через итерацию
        return s.rollbackFirstDeploy(ctx, client, current)
    }
    return s.rollbackToSnapshot(ctx, client, current)
}
```

> **Примечание о StackName:** `StackSnapshot` должен хранить `StackName` для корректного rollback.
> Добавить поле `StackName string` и заполнять его в `TakeSnapshot`.

**Полная реализация Rollback:**
```go
func (s *StackSnapshot) Rollback(ctx context.Context, client swarmint.DockerClient) error {
    // Получаем текущие сервисы стека
    current, err := swarmint.GetStackState(ctx, client, s.StackName)
    if err != nil {
        return fmt.Errorf("get current state for rollback: %w", err)
    }

    if s.IsFirstDeploy {
        return s.rollbackFirstDeploy(ctx, client, current)
    }
    return s.rollbackToSnapshot(ctx, client, current)
}

// rollbackFirstDeploy удаляет все сервисы — стека не существовало до деплоя.
func (s *StackSnapshot) rollbackFirstDeploy(ctx context.Context, client swarmint.DockerClient, current *swarmint.StackState) error {
    for _, svc := range current.Services {
        if err := client.ServiceRemove(ctx, svc.ID); err != nil {
            return fmt.Errorf("remove service %q: %w", svc.Spec.Name, err)
        }
    }
    return nil
}

// rollbackToSnapshot восстанавливает сервисы до состояния снимка.
func (s *StackSnapshot) rollbackToSnapshot(ctx context.Context, client swarmint.DockerClient, current *swarmint.StackState) error {
    // Удалить сервисы, которых не было в снимке
    for name, svc := range current.Services {
        if _, inSnapshot := s.Services[name]; !inSnapshot {
            if err := client.ServiceRemove(ctx, svc.ID); err != nil {
                return fmt.Errorf("remove new service %q: %w", name, err)
            }
        }
    }

    // Восстановить сервисы из снимка
    for name, snapshotSvc := range s.Services {
        currentSvc, exists := current.Services[name]
        if !exists {
            // Сервис удалили во время деплоя — нужно создать заново
            if _, err := client.ServiceCreate(ctx, snapshotSvc.Spec, types.ServiceCreateOptions{}); err != nil {
                return fmt.Errorf("recreate service %q: %w", name, err)
            }
            continue
        }
        // Обновить до spec из снимка
        if _, err := client.ServiceUpdate(ctx, currentSvc.ID, currentSvc.Version, snapshotSvc.Spec, types.ServiceUpdateOptions{}); err != nil {
            return fmt.Errorf("restore service %q: %w", name, err)
        }
    }
    return nil
}
```

---

### Цикл 4: Rollback при обновлении восстанавливает spec

**🔴 Тест:**
```go
func TestRollback_Update_RestoresSpec(t *testing.T) {
    oldSpec := swarm.ServiceSpec{
        Annotations: swarm.Annotations{Name: "mystack_web"},
        TaskTemplate: swarm.TaskSpec{
            ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.24"},
        },
    }
    updated := 0

    mock := &swarmint.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarm.Service, error) {
            return []swarm.Service{
                {ID: "svc1", Version: swarm.Version{Index: 5},
                    Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "mystack_web"}}},
            }, nil
        },
        ServiceUpdateFn: func(_ context.Context, id string, _ swarm.Version, spec swarm.ServiceSpec, _ types.ServiceUpdateOptions) (types.ServiceUpdateResponse, error) {
            updated++
            if spec.TaskTemplate.ContainerSpec.Image != "nginx:1.24" {
                t.Errorf("expected to restore nginx:1.24, got %q", spec.TaskTemplate.ContainerSpec.Image)
            }
            return types.ServiceUpdateResponse{}, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
    }

    snap := &snapmod.StackSnapshot{
        StackName:     "mystack",
        IsFirstDeploy: false,
        Services: map[string]swarm.Service{
            "web": {ID: "svc1", Spec: oldSpec},
        },
        Networks: map[string]network.Summary{},
        Volumes:  map[string]volume.Volume{},
    }

    err := snap.Rollback(context.Background(), mock)
    if err != nil {
        t.Fatal(err)
    }
    if updated != 1 {
        t.Errorf("expected 1 ServiceUpdate, got %d", updated)
    }
}
```

---

## Критерии завершения этапа

- [ ] `TakeSnapshot` на пустом стеке → `IsFirstDeploy=true`
- [ ] `TakeSnapshot` с сервисами → `IsFirstDeploy=false`, сервисы захвачены
- [ ] `Rollback` при `IsFirstDeploy=true` → `ServiceRemove` для всех текущих сервисов
- [ ] `Rollback` при `IsFirstDeploy=false` → `ServiceUpdate` с оригинальным spec
- [ ] `Rollback` возвращает `error`
- [ ] `go test ./internal/snapshot/...` — зелёный

## Следующий этап
→ [Этап 7: Мониторинг здоровья](./07-health-monitoring.md)
