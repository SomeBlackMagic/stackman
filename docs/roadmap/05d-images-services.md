# Этап 5г — Образы и сервисы (`internal/swarm/images.go`, `services.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 5а: DockerClient интерфейс
- [x] Этап 5б: GetStackState
- [x] Этап 2: compose.ConvertToSwarmSpec
- [x] Этап 3: deployment.DeployIDLabel

## Что создаём
```
internal/swarm/
├── images.go
├── services.go
├── images_test.go
└── services_test.go
```

---

## TDD-план

### Цикл 1: PullImages вызывает ImagePull для каждого образа

**🔴 Тест:**
```go
// internal/swarm/images_test.go
package swarm_test

import (
    "context"
    "io"
    "strings"
    "testing"

    "github.com/docker/docker/api/types/image"
    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
    "github.com/SomeBlackMagic/stackman/internal/compose"
)

func TestPullImages_CallsImagePull(t *testing.T) {
    pulled := make(map[string]bool)
    mock := &swarmint.MockDockerClient{
        ImagePullFn: func(_ context.Context, ref string, _ image.PullOptions) (io.ReadCloser, error) {
            pulled[ref] = true
            return io.NopCloser(strings.NewReader("{}")), nil
        },
    }

    services := map[string]*compose.Service{
        "web":    {Image: "nginx:latest"},
        "worker": {Image: "myapp:1.0"},
    }
    err := swarmint.PullImages(context.Background(), mock, services)
    if err != nil {
        t.Fatal(err)
    }
    if !pulled["nginx:latest"] {
        t.Error("nginx:latest should have been pulled")
    }
    if !pulled["myapp:1.0"] {
        t.Error("myapp:1.0 should have been pulled")
    }
}
```

**🟢 Реализация `images.go`:**
```go
// internal/swarm/images.go
package swarm

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "sync"

    "github.com/docker/docker/api/types/image"
    "github.com/SomeBlackMagic/stackman/internal/compose"
)

// PullImages скачивает образы всех сервисов параллельно.
// Сервисы без image (build-only) пропускаются.
func PullImages(ctx context.Context, client DockerClient, services map[string]*compose.Service) error {
    var (
        mu   sync.Mutex
        errs []error
        wg   sync.WaitGroup
    )

    for name, svc := range services {
        if svc.Image == "" {
            continue
        }
        name, svc := name, svc
        wg.Add(1)
        go func() {
            defer wg.Done()
            if err := pullImage(ctx, client, svc.Image); err != nil {
                mu.Lock()
                errs = append(errs, fmt.Errorf("pull image for service %q (%s): %w", name, svc.Image, err))
                mu.Unlock()
            }
        }()
    }
    wg.Wait()

    if len(errs) > 0 {
        return errs[0] // возвращаем первую ошибку
    }
    return nil
}

func pullImage(ctx context.Context, client DockerClient, ref string) error {
    rc, err := client.ImagePull(ctx, ref, image.PullOptions{})
    if err != nil {
        return err
    }
    defer rc.Close()
    // Читаем поток прогресса до конца (необходимо для завершения pull)
    dec := json.NewDecoder(rc)
    for {
        var msg map[string]interface{}
        if err := dec.Decode(&msg); err == io.EOF {
            break
        } else if err != nil {
            return fmt.Errorf("read pull progress: %w", err)
        }
        if errMsg, ok := msg["error"].(string); ok {
            return fmt.Errorf("pull error: %s", errMsg)
        }
    }
    return nil
}
```

---

### Цикл 2: PullImages пропускает сервисы без image

**🔴 Тест:**
```go
func TestPullImages_SkipsEmptyImage(t *testing.T) {
    pullCalled := 0
    mock := &swarmint.MockDockerClient{
        ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
            pullCalled++
            return io.NopCloser(strings.NewReader("{}")), nil
        },
    }
    services := map[string]*compose.Service{
        "built": {Image: ""}, // build-only сервис
    }
    swarmint.PullImages(context.Background(), mock, services)
    if pullCalled != 0 {
        t.Error("should not call ImagePull for service without image")
    }
}
```

---

### Цикл 3: DeployServices создаёт новый сервис

**🔴 Тест:**
```go
// internal/swarm/services_test.go
package swarm_test

func TestDeployServices_CreatesNewService(t *testing.T) {
    createCalled := 0
    var createdSpec swarm.ServiceSpec

    mock := &swarmint.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
            return nil, nil // нет существующих сервисов
        },
        ServiceCreateFn: func(_ context.Context, spec swarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (dockertypes.ServiceCreateResponse, error) {
            createCalled++
            createdSpec = spec
            return dockertypes.ServiceCreateResponse{ID: "svc1"}, nil
        },
        NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
            return []swarm.Node{{ID: "n1"}}, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
            return nil, nil
        },
    }

    services := map[string]*compose.Service{
        "web": {Image: "nginx:latest"},
    }
    results, err := swarmint.DeployServices(context.Background(), mock, "mystack", services, "deploy-20260101-000000-abc12345")
    if err != nil {
        t.Fatal(err)
    }
    if createCalled != 1 {
        t.Errorf("expected 1 ServiceCreate, got %d", createCalled)
    }
    if len(results) != 1 || !results[0].Created {
        t.Error("expected Created=true in result")
    }
    // Проверяем DeployID label
    if createdSpec.Labels[deployment.DeployIDLabel] != "deploy-20260101-000000-abc12345" {
        t.Error("deploy ID label not set on service")
    }
}
```

**🟢 Реализация `services.go`:**
```go
// internal/swarm/services.go
package swarm

import (
    "context"
    "fmt"

    dockertypes "github.com/docker/docker/api/types"
    "github.com/docker/docker/api/types/filters"
    "github.com/docker/docker/api/types/swarm"
    "github.com/SomeBlackMagic/stackman/internal/compose"
    "github.com/SomeBlackMagic/stackman/internal/deployment"
)

type ServiceUpdateResult struct {
    ServiceID   string
    ServiceName string
    Version     swarm.Version
    Warnings    []string
    Created     bool
    DeployID    string
}

// DeployServices создаёт или обновляет все сервисы стека.
// Инъектирует deployID как label com.stackman.deploy.id.
func DeployServices(
    ctx context.Context,
    client DockerClient,
    stackName string,
    services map[string]*compose.Service,
    deployID string,
) ([]ServiceUpdateResult, error) {
    // Получаем текущие сервисы
    existing, err := client.ServiceList(ctx, dockertypes.ServiceListOptions{
        Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
    })
    if err != nil {
        return nil, fmt.Errorf("list existing services: %w", err)
    }
    existingByName := make(map[string]swarm.Service)
    prefix := stackName + "_"
    for _, svc := range existing {
        existingByName[svc.Spec.Name] = svc
    }

    // Определяем single/multi node для host-gateway
    isMulti, err := IsMultiNodeSwarm(ctx, client)
    if err != nil {
        return nil, fmt.Errorf("detect swarm mode: %w", err)
    }
    var gatewayIP string
    if !isMulti {
        gatewayIP, _ = DetectHostGatewayIP(ctx, client) // ошибку игнорируем если нет docker0
    }

    var results []ServiceUpdateResult
    for name, svc := range services {
        result, err := deployService(ctx, client, name, svc, stackName, deployID, gatewayIP, existingByName)
        if err != nil {
            return nil, fmt.Errorf("deploy service %q: %w", name, err)
        }
        results = append(results, *result)
    }
    return results, nil
}

func deployService(
    ctx context.Context,
    client DockerClient,
    name string,
    svc *compose.Service,
    stackName, deployID, gatewayIP string,
    existing map[string]swarm.Service,
) (*ServiceUpdateResult, error) {
    fullName := stackName + "_" + name

    // Резолвим host-gateway в extra_hosts
    if gatewayIP != "" && compose.HasHostGateway(svc.ExtraHosts) {
        svc.ExtraHosts = compose.ReplaceHostGatewayToken(svc.ExtraHosts, gatewayIP)
    }

    spec, err := compose.ConvertToSwarmSpec(name, *svc, stackName)
    if err != nil {
        return nil, fmt.Errorf("convert spec: %w", err)
    }

    // Инъекция deployID
    if spec.Labels == nil {
        spec.Labels = make(map[string]string)
    }
    spec.Labels[deployment.DeployIDLabel] = deployID
    if spec.TaskTemplate.ContainerSpec != nil {
        if spec.TaskTemplate.ContainerSpec.Labels == nil {
            spec.TaskTemplate.ContainerSpec.Labels = make(map[string]string)
        }
        spec.TaskTemplate.ContainerSpec.Labels[deployment.DeployIDLabel] = deployID
    }

    result := &ServiceUpdateResult{
        ServiceName: fullName,
        DeployID:    deployID,
    }

    if existingSvc, ok := existing[fullName]; ok {
        // Обновляем существующий
        resp, err := client.ServiceUpdate(ctx, existingSvc.ID, existingSvc.Version, spec, dockertypes.ServiceUpdateOptions{})
        if err != nil {
            return nil, fmt.Errorf("service update: %w", err)
        }
        result.ServiceID = existingSvc.ID
        result.Version = existingSvc.Version
        result.Warnings = resp.Warnings
        result.Created = false
    } else {
        // Создаём новый
        resp, err := client.ServiceCreate(ctx, spec, dockertypes.ServiceCreateOptions{})
        if err != nil {
            return nil, fmt.Errorf("service create: %w", err)
        }
        result.ServiceID = resp.ID
        result.Created = true
    }
    return result, nil
}
```

---

### Цикл 4: DeployServices обновляет существующий сервис

**🔴 Тест:**
```go
func TestDeployServices_UpdatesExisting(t *testing.T) {
    updateCalled := 0
    existingVersion := swarm.Version{Index: 42}

    mock := &swarmint.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
            return []swarm.Service{
                {
                    ID:      "svc-existing",
                    Version: existingVersion,
                    Spec: swarm.ServiceSpec{
                        Annotations: swarm.Annotations{Name: "mystack_web"},
                    },
                },
            }, nil
        },
        ServiceUpdateFn: func(_ context.Context, id string, v swarm.Version, _ swarm.ServiceSpec, _ dockertypes.ServiceUpdateOptions) (dockertypes.ServiceUpdateResponse, error) {
            updateCalled++
            if id != "svc-existing" {
                t.Errorf("expected id svc-existing, got %q", id)
            }
            if v.Index != 42 {
                t.Errorf("expected version 42, got %d", v.Index)
            }
            return dockertypes.ServiceUpdateResponse{}, nil
        },
        NodeListFn:    func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) { return []swarm.Node{{ID: "n1"}}, nil },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
    }

    services := map[string]*compose.Service{"web": {Image: "nginx:1.26"}}
    results, err := swarmint.DeployServices(context.Background(), mock, "mystack", services, "deploy-id")
    if err != nil {
        t.Fatal(err)
    }
    if updateCalled != 1 {
        t.Errorf("expected 1 ServiceUpdate call, got %d", updateCalled)
    }
    if results[0].Created {
        t.Error("expected Created=false for update")
    }
}
```

---

## Критерии завершения этапа

- [ ] `PullImages` вызывает `ImagePull` для каждого сервиса с image
- [ ] `PullImages` пропускает сервисы без image
- [ ] `DeployServices` вызывает `ServiceCreate` для нового сервиса
- [ ] `DeployServices` вызывает `ServiceUpdate` для существующего с правильным Version
- [ ] Результат `Created=true` для нового, `false` для обновления
- [ ] Label `com.stackman.deploy.id` инъектирован в ServiceSpec и TaskTemplate
- [ ] `go test ./internal/swarm/ -run TestPullImages,TestDeployServices` — зелёный

## Следующий этап
→ [Этап 5д: Очистка ресурсов](./05e-cleanup.md)
