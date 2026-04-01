# Этап 5е — Оркестратор деплоя (`internal/swarm/stack.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 5б: GetStackState
- [x] Этап 5в: EnsureNetworks, EnsureVolumes
- [x] Этап 5г: PullImages, DeployServices
- [x] Этап 5д: RemoveExitedContainers, PruneOrphanedServices

## Что создаём
```
internal/swarm/
├── stack.go
└── stack_test.go
```

---

## TDD-план

### Цикл 1: Deploy вызывает шаги в правильном порядке

**🔴 Тест:**
```go
// internal/swarm/stack_test.go
package swarm_test

import (
    "context"
    "testing"
    "io"
    "strings"

    "github.com/docker/docker/api/types/image"
    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
    "github.com/SomeBlackMagic/stackman/internal/compose"
)

func TestStackDeployer_Deploy_StepOrder(t *testing.T) {
    var steps []string

    mock := &swarmint.MockDockerClient{
        ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]types.Container, error) {
            steps = append(steps, "container_list")
            return nil, nil
        },
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarm.Service, error) {
            steps = append(steps, "service_list")
            return nil, nil
        },
        ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
            steps = append(steps, "image_pull")
            return io.NopCloser(strings.NewReader("{}")), nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
            steps = append(steps, "network_list")
            return nil, nil
        },
        NetworkCreateFn: func(_ context.Context, _ string, _ network.CreateOptions) (network.CreateResponse, error) {
            steps = append(steps, "network_create")
            return network.CreateResponse{}, nil
        },
        VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
            steps = append(steps, "volume_list")
            return volume.ListResponse{}, nil
        },
        ServiceCreateFn: func(_ context.Context, _ swarm.ServiceSpec, _ types.ServiceCreateOptions) (types.ServiceCreateResponse, error) {
            steps = append(steps, "service_create")
            return types.ServiceCreateResponse{ID: "svc1"}, nil
        },
        NodeListFn: func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) {
            return []swarm.Node{{ID: "n1"}}, nil
        },
    }

    cf := &compose.ComposeFile{
        Services: map[string]compose.Service{"web": {Image: "nginx:latest"}},
        Networks: map[string]compose.Network{"default": {}},
    }

    deployer := swarmint.NewStackDeployer(mock, "mystack")
    _, err := deployer.Deploy(context.Background(), cf, "deploy-id", swarmint.DeployOptions{})
    if err != nil {
        t.Fatal(err)
    }

    // Проверяем порядок ключевых шагов
    orderChecks := []struct{ before, after string }{
        {"container_list", "image_pull"},
        {"image_pull", "network_list"},
        {"network_list", "service_create"},
    }
    pos := func(step string) int {
        for i, s := range steps {
            if s == step { return i }
        }
        return -1
    }
    for _, check := range orderChecks {
        if pos(check.before) > pos(check.after) {
            t.Errorf("step %q should come before %q, got: %v", check.before, check.after, steps)
        }
    }
}
```

**🟢 Реализация `stack.go`:**
```go
// internal/swarm/stack.go
package swarm

import (
    "context"
    "fmt"

    "github.com/SomeBlackMagic/stackman/internal/compose"
)

type DeployOptions struct {
    PruneOrphans bool
    NoPull       bool
}

type DeploymentResult struct {
    DeployID        string
    UpdatedServices []ServiceUpdateResult
}

type StackDeployer struct {
    client    DockerClient
    stackName string
}

func NewStackDeployer(client DockerClient, stackName string) *StackDeployer {
    return &StackDeployer{client: client, stackName: stackName}
}

// Deploy выполняет полный деплой стека в детерминированном порядке:
// 1. RemoveExitedContainers
// 2. PruneOrphanedServices (если opts.PruneOrphans)
// 3. PullImages (если !opts.NoPull)
// 4. EnsureNetworks
// 5. EnsureVolumes
// 6. DeployServices
func (d *StackDeployer) Deploy(ctx context.Context, cf *compose.ComposeFile, deployID string, opts DeployOptions) (*DeploymentResult, error) {
    // 1. Очистка завершённых контейнеров
    if err := RemoveExitedContainers(ctx, d.client, d.stackName); err != nil {
        return nil, fmt.Errorf("remove exited containers: %w", err)
    }

    // 2. Удаление orphaned сервисов (только с --prune)
    if opts.PruneOrphans {
        desired := make(map[string]bool)
        for name := range cf.Services {
            desired[name] = true
        }
        if err := PruneOrphanedServices(ctx, d.client, d.stackName, desired); err != nil {
            return nil, fmt.Errorf("prune orphaned services: %w", err)
        }
        desiredNets := make(map[string]bool)
        for name := range cf.Networks {
            desiredNets[name] = true
        }
        if err := PruneOrphanedNetworks(ctx, d.client, d.stackName, desiredNets); err != nil {
            return nil, fmt.Errorf("prune orphaned networks: %w", err)
        }
    }

    // 3. Pull образов
    if !opts.NoPull {
        svcs := make(map[string]*compose.Service)
        for k, v := range cf.Services {
            v := v
            svcs[k] = &v
        }
        if err := PullImages(ctx, d.client, svcs); err != nil {
            return nil, fmt.Errorf("pull images: %w", err)
        }
    }

    // 4. Сети
    nets := make(map[string]*compose.Network)
    for k, v := range cf.Networks {
        v := v
        nets[k] = &v
    }
    if err := EnsureNetworks(ctx, d.client, d.stackName, nets); err != nil {
        return nil, fmt.Errorf("ensure networks: %w", err)
    }

    // 5. Тома
    vols := make(map[string]*compose.Volume)
    for k, v := range cf.Volumes {
        v := v
        vols[k] = &v
    }
    if err := EnsureVolumes(ctx, d.client, d.stackName, vols); err != nil {
        return nil, fmt.Errorf("ensure volumes: %w", err)
    }

    // 6. Сервисы
    svcs := make(map[string]*compose.Service)
    for k, v := range cf.Services {
        v := v
        svcs[k] = &v
    }
    results, err := DeployServices(ctx, d.client, d.stackName, svcs, deployID)
    if err != nil {
        return nil, fmt.Errorf("deploy services: %w", err)
    }

    return &DeploymentResult{
        DeployID:        deployID,
        UpdatedServices: results,
    }, nil
}

// GetState возвращает текущее состояние стека.
func (d *StackDeployer) GetState(ctx context.Context) (*StackState, error) {
    return GetStackState(ctx, d.client, d.stackName)
}
```

---

### Цикл 2: Ошибка на шаге PullImages останавливает деплой

**🔴 Тест:**
```go
func TestStackDeployer_Deploy_StopsOnPullError(t *testing.T) {
    serviceCreateCalled := 0
    mock := &swarmint.MockDockerClient{
        ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]types.Container, error) { return nil, nil },
        ServiceListFn:   func(_ context.Context, _ types.ServiceListOptions) ([]swarm.Service, error) { return nil, nil },
        NetworkListFn:   func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:    func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
        ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
            return nil, errors.New("image not found")
        },
        ServiceCreateFn: func(_ context.Context, _ swarm.ServiceSpec, _ types.ServiceCreateOptions) (types.ServiceCreateResponse, error) {
            serviceCreateCalled++
            return types.ServiceCreateResponse{}, nil
        },
        NodeListFn: func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) { return []swarm.Node{{ID: "n1"}}, nil },
    }

    cf := &compose.ComposeFile{
        Services: map[string]compose.Service{"web": {Image: "nonexistent:latest"}},
    }
    deployer := swarmint.NewStackDeployer(mock, "mystack")
    _, err := deployer.Deploy(context.Background(), cf, "deploy-id", swarmint.DeployOptions{})
    if err == nil {
        t.Fatal("expected error from PullImages")
    }
    if serviceCreateCalled != 0 {
        t.Error("ServiceCreate should not be called after PullImages fails")
    }
    // Ошибка должна содержать контекст
    if !strings.Contains(err.Error(), "pull images") {
        t.Errorf("error should mention 'pull images', got: %v", err)
    }
}
```

---

### Цикл 3: Ошибки оборачиваются через %w

**🔴 Тест:**
```go
func TestStackDeployer_ErrorWrapping(t *testing.T) {
    baseErr := errors.New("docker connection refused")
    mock := &swarmint.MockDockerClient{
        ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]types.Container, error) {
            return nil, baseErr
        },
    }
    deployer := swarmint.NewStackDeployer(mock, "mystack")
    cf := &compose.ComposeFile{}
    _, err := deployer.Deploy(context.Background(), cf, "deploy-id", swarmint.DeployOptions{})
    if !errors.Is(err, baseErr) {
        t.Errorf("expected errors.Is to find base error, got: %v", err)
    }
}
```

---

## Критерии завершения этапа

- [ ] `Deploy` вызывает шаги в порядке: cleanup → prune → pull → networks → volumes → services
- [ ] При ошибке `PullImages` — `DeployServices` не вызывается
- [ ] Все ошибки оборачиваются через `%w` — `errors.Is` находит оригинальную ошибку
- [ ] `--prune` вызывает `PruneOrphanedServices` и `PruneOrphanedNetworks`
- [ ] `--no-pull` пропускает `PullImages`
- [ ] `GetState` делегирует `GetStackState`
- [ ] `go test ./internal/swarm/...` — все тесты зелёные

## Следующий этап
→ [Этап 6: Снимки состояния](./06-snapshots.md)
