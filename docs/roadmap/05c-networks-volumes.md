# Этап 5в — Сети и тома (`internal/swarm/networks.go`, `volumes.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 5а: DockerClient интерфейс
- [x] Этап 5б: GetStackState
- [x] Этап 2: compose.Network, compose.Volume

## Что создаём
```
internal/swarm/
├── networks.go
├── volumes.go
├── networks_test.go
└── volumes_test.go
```

---

## Принцип: идемпотентность

> `EnsureNetworks` и `EnsureVolumes` — идемпотентные операции.
> При повторном вызове с теми же аргументами не создают дубликаты.
> Имена ресурсов: `<stackName>_<resourceName>`.

---

## TDD-план

### Цикл 1: EnsureNetworks создаёт отсутствующую сеть

**🔴 Тест:**
```go
// internal/swarm/networks_test.go
package swarm_test

import (
    "context"
    "testing"

    "github.com/docker/docker/api/types/network"
    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
    "github.com/SomeBlackMagic/stackman/internal/compose"
)

func TestEnsureNetworks_CreatesNew(t *testing.T) {
    createCalled := 0
    mock := &swarmint.MockDockerClient{
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
            return nil, nil // нет существующих сетей
        },
        NetworkCreateFn: func(_ context.Context, name string, opts network.CreateOptions) (network.CreateResponse, error) {
            createCalled++
            if name != "mystack_frontend" {
                t.Errorf("expected network name mystack_frontend, got %q", name)
            }
            return network.CreateResponse{ID: "net1"}, nil
        },
    }

    nets := map[string]*compose.Network{
        "frontend": {Driver: "overlay"},
    }
    err := swarmint.EnsureNetworks(context.Background(), mock, "mystack", nets)
    if err != nil {
        t.Fatal(err)
    }
    if createCalled != 1 {
        t.Errorf("expected 1 NetworkCreate call, got %d", createCalled)
    }
}
```

**🟢 Реализация `networks.go`:**
```go
// internal/swarm/networks.go
package swarm

import (
    "context"
    "fmt"

    "github.com/docker/docker/api/types/filters"
    "github.com/docker/docker/api/types/network"
    "github.com/SomeBlackMagic/stackman/internal/compose"
)

// EnsureNetworks создаёт сети из compose-файла если они не существуют.
// Идемпотентен: существующие сети не трогает.
func EnsureNetworks(ctx context.Context, client DockerClient, stackName string, networks map[string]*compose.Network) error {
    // Получаем список существующих сетей стека
    existing, err := client.NetworkList(ctx, network.ListOptions{
        Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
    })
    if err != nil {
        return fmt.Errorf("list networks: %w", err)
    }

    existingByName := make(map[string]bool)
    prefix := stackName + "_"
    for _, net := range existing {
        existingByName[net.Name] = true
    }

    for name, net := range networks {
        fullName := prefix + name
        if existingByName[fullName] {
            continue // уже существует
        }
        opts, err := compose.ConvertNetwork(name, *net, stackName)
        if err != nil {
            return fmt.Errorf("convert network %q: %w", name, err)
        }
        if _, err := client.NetworkCreate(ctx, fullName, opts); err != nil {
            return fmt.Errorf("create network %q: %w", fullName, err)
        }
    }
    return nil
}
```

---

### Цикл 2: EnsureNetworks — идемпотентность (уже существует)

**🔴 Тест:**
```go
func TestEnsureNetworks_SkipsExisting(t *testing.T) {
    createCalled := 0
    mock := &swarmint.MockDockerClient{
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
            return []network.Summary{
                {Name: "mystack_frontend", ID: "net1"},
            }, nil
        },
        NetworkCreateFn: func(_ context.Context, _ string, _ network.CreateOptions) (network.CreateResponse, error) {
            createCalled++
            return network.CreateResponse{}, nil
        },
    }

    nets := map[string]*compose.Network{
        "frontend": {Driver: "overlay"},
    }
    err := swarmint.EnsureNetworks(context.Background(), mock, "mystack", nets)
    if err != nil {
        t.Fatal(err)
    }
    if createCalled != 0 {
        t.Errorf("NetworkCreate should not be called for existing network, got %d calls", createCalled)
    }
}
```

---

### Цикл 3: Внешние сети (external) пропускаются

**🔴 Тест:**
```go
func TestEnsureNetworks_SkipsExternal(t *testing.T) {
    createCalled := 0
    mock := &swarmint.MockDockerClient{
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
            return nil, nil
        },
        NetworkCreateFn: func(_ context.Context, _ string, _ network.CreateOptions) (network.CreateResponse, error) {
            createCalled++
            return network.CreateResponse{}, nil
        },
    }

    nets := map[string]*compose.Network{
        "external-net": {External: true},
    }
    err := swarmint.EnsureNetworks(context.Background(), mock, "mystack", nets)
    if err != nil {
        t.Fatal(err)
    }
    if createCalled != 0 {
        t.Errorf("external network should not be created")
    }
}
```

**🟢 Добавить в `EnsureNetworks`:**
```go
// Перед созданием проверяем External
if isExternalNetwork(net) {
    continue
}
```

```go
func isExternalNetwork(net *compose.Network) bool {
    if net.External == nil {
        return false
    }
    switch v := net.External.(type) {
    case bool:
        return v
    case map[string]interface{}:
        return true // external: {name: ...}
    }
    return false
}
```

---

### Цикл 4: EnsureVolumes создаёт отсутствующий том

**🔴 Тест:**
```go
// internal/swarm/volumes_test.go
package swarm_test

func TestEnsureVolumes_CreatesNew(t *testing.T) {
    createCalled := 0
    mock := &swarmint.MockDockerClient{
        VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
            return volume.ListResponse{}, nil
        },
        VolumeCreateFn: func(_ context.Context, opts volume.CreateOptions) (volume.Volume, error) {
            createCalled++
            if opts.Name != "mystack_data" {
                t.Errorf("expected volume name mystack_data, got %q", opts.Name)
            }
            return volume.Volume{Name: opts.Name}, nil
        },
    }

    vols := map[string]*compose.Volume{
        "data": {Driver: "local"},
    }
    err := swarmint.EnsureVolumes(context.Background(), mock, "mystack", vols)
    if err != nil {
        t.Fatal(err)
    }
    if createCalled != 1 {
        t.Errorf("expected 1 VolumeCreate call, got %d", createCalled)
    }
}
```

**🟢 Реализация `volumes.go`:**
```go
// internal/swarm/volumes.go
package swarm

import (
    "context"
    "fmt"
    "strings"

    "github.com/docker/docker/api/types/filters"
    "github.com/docker/docker/api/types/volume"
    "github.com/SomeBlackMagic/stackman/internal/compose"
)

func EnsureVolumes(ctx context.Context, client DockerClient, stackName string, volumes map[string]*compose.Volume) error {
    existing, err := client.VolumeList(ctx, filters.NewArgs(
        filters.Arg("label", "com.docker.stack.namespace="+stackName),
    ))
    if err != nil {
        return fmt.Errorf("list volumes: %w", err)
    }

    existingByName := make(map[string]bool)
    prefix := stackName + "_"
    for _, vol := range existing.Volumes {
        existingByName[vol.Name] = true
    }

    for name, vol := range volumes {
        // Внешние тома не создаём
        if isExternalVolume(vol) {
            continue
        }
        fullName := prefix + name
        if existingByName[fullName] {
            continue
        }
        opts, err := compose.ConvertVolume(name, *vol, stackName)
        if err != nil {
            return fmt.Errorf("convert volume %q: %w", name, err)
        }
        opts.Name = fullName
        if _, err := client.VolumeCreate(ctx, opts); err != nil {
            return fmt.Errorf("create volume %q: %w", fullName, err)
        }
    }
    return nil
}

func isExternalVolume(vol *compose.Volume) bool {
    if vol.External == nil {
        return false
    }
    switch v := vol.External.(type) {
    case bool:
        return v
    case map[string]interface{}:
        return true
    default:
        _ = v
        return false
    }
}
```

### Цикл 5: EnsureVolumes — идемпотентность

**🔴 Тест:**
```go
func TestEnsureVolumes_SkipsExisting(t *testing.T) {
    createCalled := 0
    mock := &swarmint.MockDockerClient{
        VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
            return volume.ListResponse{
                Volumes: []*volume.Volume{{Name: "mystack_data"}},
            }, nil
        },
        VolumeCreateFn: func(_ context.Context, _ volume.CreateOptions) (volume.Volume, error) {
            createCalled++
            return volume.Volume{}, nil
        },
    }
    vols := map[string]*compose.Volume{"data": {}}
    swarmint.EnsureVolumes(context.Background(), mock, "mystack", vols)
    if createCalled != 0 {
        t.Errorf("VolumeCreate should not be called for existing volume")
    }
}
```

---

## Критерии завершения этапа

- [ ] `EnsureNetworks` вызывает `NetworkCreate` только для отсутствующих сетей
- [ ] `EnsureNetworks` — повторный вызов не делает `NetworkCreate`
- [ ] `EnsureNetworks` пропускает external сети
- [ ] Имена: `<stackName>_<name>` (например `mystack_frontend`)
- [ ] `EnsureVolumes` аналогично для томов
- [ ] `go test ./internal/swarm/ -run TestEnsureNetworks,TestEnsureVolumes` — зелёный

## Следующий этап
→ [Этап 5г: Образы и сервисы](./05d-images-services.md)
