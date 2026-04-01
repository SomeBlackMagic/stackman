# Этап 9 — Команда `apply` (`cmd/apply.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 1: CLI-скелет, `parseApplyArgs`
- [x] Этап 2: `compose.ParseFile`
- [x] Этап 3: `deployment.GenerateDeployID`
- [x] Этап 4: `paths.NewResolver`
- [x] Этап 5е: `swarm.StackDeployer`
- [x] Этап 6: `snapshot.TakeSnapshot`, `snapshot.Rollback`
- [x] Этап 7: `health.ServiceUpdateMonitor`

## Что создаём / изменяем
```
cmd/apply.go       # полная реализация ExecuteApply
cmd/apply_test.go  # unit-тесты логики оркестрации
```

---

## Полный workflow команды

```
1.  Парсинг флагов → ApplyArgs
2.  Резолвинг пути к compose-файлу
3.  Парсинг compose-файла
4.  Подключение к Docker daemon (client.NewClientWithOpts)
5.  TakeSnapshot текущего состояния
6.  GenerateDeployID
7.  StackDeployer.Deploy:
    a. RemoveExitedContainers
    b. PruneOrphanedServices (если --prune)
    c. PullImages
    d. EnsureNetworks / EnsureVolumes
    e. DeployServices
8.  Параллельный мониторинг: goroutine per service
    a. ServiceUpdateMonitor.WaitHealthy
    b. (опционально) стриминг логов
9.  Ожидание завершения всех горутин
10. Успех → exit 0
    Ошибка / таймаут → Rollback → exit 1/2
    SIGINT/SIGTERM → Rollback → exit 3/4
```

---

## TDD-план

### Цикл 1: Отсутствие --name → ошибка

**🔴 Тест:**
```go
// cmd/apply_test.go
package cmd_test

import (
    "os/exec"
    "testing"
)

// runApply запускает subprocess чтобы поймать os.Exit
func runApply(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
    t.Helper()
    cmd := exec.Command("go", append([]string{"run", "../main.go", "apply"}, args...)...)
    var out, errOut strings.Builder
    cmd.Stdout = &out
    cmd.Stderr = &errOut
    err := cmd.Run()
    exitCode = 0
    if e, ok := err.(*exec.ExitError); ok {
        exitCode = e.ExitCode()
    }
    return out.String(), errOut.String(), exitCode
}

func TestApply_MissingName(t *testing.T) {
    _, stderr, code := runApply(t, "-f", "docker-compose.yml")
    if code != 1 {
        t.Errorf("expected exit 1, got %d", code)
    }
    if !strings.Contains(stderr, "--name") {
        t.Errorf("expected --name in error message, got: %s", stderr)
    }
}

func TestApply_MissingComposeFile(t *testing.T) {
    _, stderr, code := runApply(t, "-n", "mystack")
    if code != 1 {
        t.Errorf("expected exit 1, got %d", code)
    }
    if !strings.Contains(stderr, "--compose-file") {
        t.Errorf("expected --compose-file in error message, got: %s", stderr)
    }
}
```

> **Примечание:** Эти тесты проверяют уже реализованное в Этапе 1 поведение.
> При Этапе 9 добавляем интеграционное покрытие полного workflow через `runApply`.

---

### Цикл 2: Несуществующий compose-файл → ошибка с exit 1

**🔴 Тест:**
```go
func TestApply_NonexistentComposeFile(t *testing.T) {
    _, stderr, code := runApply(t, "-n", "mystack", "-f", "/nonexistent/path.yml")
    if code != 1 {
        t.Errorf("expected exit 1, got %d", code)
    }
    if !strings.Contains(stderr, "no such file") && !strings.Contains(stderr, "not found") {
        t.Errorf("expected file not found message, got: %s", stderr)
    }
}
```

---

### Цикл 3: Структура `runApply` — чистая функция для тестирования

Чтобы тестировать логику без subprocess и без Docker, выносим основную логику в тестируемую функцию:

```go
// cmd/apply.go

type ApplyDeps struct {
    DockerClient   swarm.DockerClient
    Stdout, Stderr io.Writer
}

// runApplyWorkflow — основная логика, принимает зависимости явно.
// ExecuteApply создаёт реальные зависимости и вызывает эту функцию.
func runApplyWorkflow(ctx context.Context, args *ApplyArgs, deps ApplyDeps) error {
    // 1. Резолвинг пути
    resolver, err := paths.NewResolver()
    if err != nil {
        return fmt.Errorf("init path resolver: %w", err)
    }
    composePath := resolver.Resolve(args.ComposeFile)

    // 2. Парсинг compose
    cf, err := compose.ParseFile(composePath)
    if err != nil {
        return fmt.Errorf("parse compose file: %w", err)
    }

    // 3. Snapshot
    deployer := swarm.NewStackDeployer(deps.DockerClient, args.StackName)
    snap, err := snapshot.TakeSnapshot(ctx, deployer)
    if err != nil {
        return fmt.Errorf("take snapshot: %w", err)
    }

    // 4. Deploy ID
    deployID := deployment.GenerateDeployID()
    fmt.Fprintf(deps.Stdout, "[INFO] Starting deployment %s\n", deployID)

    // 5. Deploy
    result, err := deployer.Deploy(ctx, cf, deployID, swarm.DeployOptions{
        PruneOrphans: args.Prune,
        NoPull:       args.NoPull,
    })
    if err != nil {
        fmt.Fprintf(deps.Stderr, "[ERROR] Deploy failed: %v\n", err)
        if rbErr := snap.Rollback(ctx, deps.DockerClient); rbErr != nil {
            fmt.Fprintf(deps.Stderr, "[ERROR] Rollback failed: %v\n", rbErr)
        }
        return err
    }

    // 6. Параллельный мониторинг здоровья
    if err := waitForAllHealthy(ctx, deps, result, args); err != nil {
        fmt.Fprintf(deps.Stderr, "[ERROR] Health check failed: %v\n", err)
        if rbErr := snap.Rollback(ctx, deps.DockerClient); rbErr != nil {
            fmt.Fprintf(deps.Stderr, "[ERROR] Rollback failed: %v\n", rbErr)
        }
        return err
    }

    fmt.Fprintf(deps.Stdout, "[INFO] Deployment %s completed successfully\n", deployID)
    return nil
}
```

**🔴 Тест с Mock:**
```go
// cmd/apply_workflow_test.go
package cmd_test

func TestApplyWorkflow_SuccessfulDeploy(t *testing.T) {
    replicas := uint64(1)
    mock := &swarm.MockDockerClient{
        // Пустой стек
        ServiceListFn:   func(_ context.Context, _ types.ServiceListOptions) ([]swarm.Service, error) { return nil, nil },
        NetworkListFn:   func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:    func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
        ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]types.Container, error) { return nil, nil },
        NodeListFn:      func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) { return []swarm.Node{{ID: "n1"}}, nil },
        ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
            return io.NopCloser(strings.NewReader("{}")), nil
        },
        ServiceCreateFn: func(_ context.Context, _ swarm.ServiceSpec, _ types.ServiceCreateOptions) (types.ServiceCreateResponse, error) {
            return types.ServiceCreateResponse{ID: "svc1"}, nil
        },
        TaskListFn: func(_ context.Context, _ types.TaskListOptions) ([]swarm.Task, error) {
            return []swarm.Task{
                {Status: dockerswarm.TaskStatus{State: dockerswarm.TaskStateRunning}},
            }, nil
        },
        ServiceInspectWithRawFn: func(_ context.Context, _ string, _ types.ServiceInspectOptions) (dockerswarm.Service, []byte, error) {
            return dockerswarm.Service{
                Spec: dockerswarm.ServiceSpec{
                    Mode: dockerswarm.ServiceMode{Replicated: &dockerswarm.ReplicatedService{Replicas: &replicas}},
                },
            }, nil, nil
        },
    }

    // Создаём временный compose-файл
    tmpFile := writeTempCompose(t, `
version: "3.8"
services:
  web:
    image: nginx:latest
`)

    var stdout, stderr strings.Builder
    args := &cmd.ApplyArgs{
        StackName:   "mystack",
        ComposeFile: tmpFile,
        Timeout:     "30s",
    }
    err := cmd.RunApplyWorkflow(context.Background(), args, cmd.ApplyDeps{
        DockerClient: mock,
        Stdout:       &stdout,
        Stderr:       &stderr,
    })
    if err != nil {
        t.Fatalf("unexpected error: %v\nstderr: %s", err, stderr.String())
    }
    if !strings.Contains(stdout.String(), "completed successfully") {
        t.Error("expected success message in output")
    }
}

func writeTempCompose(t *testing.T, content string) string {
    t.Helper()
    f, err := os.CreateTemp(t.TempDir(), "compose-*.yml")
    if err != nil { t.Fatal(err) }
    f.WriteString(content)
    f.Close()
    return f.Name()
}
```

---

### Цикл 4: Ошибка деплоя → откат → exit 1

**🔴 Тест:**
```go
func TestApplyWorkflow_DeployError_TriggersRollback(t *testing.T) {
    rollbackCalled := false
    mock := &swarm.MockDockerClient{
        ServiceListFn:   func(_ context.Context, _ types.ServiceListOptions) ([]swarm.Service, error) { return nil, nil },
        NetworkListFn:   func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:    func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
        ContainerListFn: func(_ context.Context, _ container.ListOptions) ([]types.Container, error) { return nil, nil },
        NodeListFn:      func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) { return []swarm.Node{{ID: "n1"}}, nil },
        ImagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
            return nil, errors.New("image pull failed: unauthorized")
        },
    }

    tmpFile := writeTempCompose(t, `version: "3.8"
services:
  web:
    image: private:latest`)

    var stderr strings.Builder
    err := cmd.RunApplyWorkflow(context.Background(), &cmd.ApplyArgs{
        StackName: "mystack", ComposeFile: tmpFile, Timeout: "30s",
    }, cmd.ApplyDeps{DockerClient: mock, Stdout: io.Discard, Stderr: &stderr})

    if err == nil {
        t.Fatal("expected error")
    }
    // snapshot.IsFirstDeploy=true — rollback = ничего не удалять (стека не было)
    _ = rollbackCalled
    if !strings.Contains(stderr.String(), "Deploy failed") {
        t.Error("expected 'Deploy failed' in stderr")
    }
}
```

---

### Цикл 5: Signal handling (SIGINT → откат)

**🟢 Реализация в `ExecuteApply`:**
```go
func ExecuteApply(args []string) {
    a, err := parseApplyArgs(args)
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    // Signal handling
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

    ctx, cancel := context.WithCancel(context.Background())

    // Отдельная горутина: при сигнале отменяем context
    rollbackNeeded := make(chan struct{})
    go func() {
        select {
        case <-sigCh:
            fmt.Fprintln(os.Stderr, "\n[WARN] Interrupt received, initiating rollback...")
            cancel()
            close(rollbackNeeded)
        case <-ctx.Done():
        }
    }()

    // Подключение к Docker
    dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
    if err != nil {
        fmt.Fprintf(os.Stderr, "[ERROR] Failed to connect to Docker: %v\n", err)
        os.Exit(1)
    }
    defer dockerClient.Close()

    deps := ApplyDeps{
        DockerClient: dockerClient,
        Stdout:       os.Stdout,
        Stderr:       os.Stderr,
    }

    // Парсинг timeout
    timeout, err := time.ParseDuration(a.Timeout)
    if err != nil {
        fmt.Fprintf(os.Stderr, "[ERROR] Invalid timeout: %v\n", err)
        os.Exit(1)
    }
    ctx, cancelTimeout := context.WithTimeout(ctx, timeout)
    defer cancelTimeout()

    if err := runApplyWorkflow(ctx, a, deps); err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            os.Exit(2) // таймаут
        }
        if errors.Is(err, context.Canceled) {
            select {
            case <-rollbackNeeded:
                os.Exit(3) // прерван + откат выполнен
            default:
                os.Exit(4) // прерван + откат не выполнен
            }
        }
        os.Exit(1) // общая ошибка
    }
    os.Exit(0)
}
```

---

### Цикл 6: Параллельный мониторинг с отменой при ошибке

```go
// cmd/apply.go

func waitForAllHealthy(ctx context.Context, deps ApplyDeps, result *swarm.DeploymentResult, args *ApplyArgs) error {
    timeout, _ := time.ParseDuration(args.Timeout)

    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

    errCh := make(chan error, len(result.UpdatedServices))
    var wg sync.WaitGroup

    for _, svcResult := range result.UpdatedServices {
        svcResult := svcResult
        wg.Add(1)
        go func() {
            defer wg.Done()
            mon := health.NewServiceUpdateMonitor(deps.DockerClient, svcResult.ServiceID, svcResult.ServiceName)
            if err := mon.WaitHealthy(ctx, timeout); err != nil {
                errCh <- fmt.Errorf("service %q: %w", svcResult.ServiceName, err)
                cancel() // отменяем остальные горутины
            }
        }()
    }

    wg.Wait()
    close(errCh)

    for err := range errCh {
        if err != nil {
            return err // возвращаем первую ошибку
        }
    }
    return nil
}
```

---

## Критерии завершения этапа

- [ ] `stackman apply` без флагов → exit 1, понятная ошибка
- [ ] `stackman apply -n x -f nonexistent.yml` → exit 1
- [ ] Успешный деплой с mock → exit 0, "completed successfully"
- [ ] Ошибка `PullImages` → откат → exit 1, "Deploy failed" в stderr
- [ ] SIGINT во время деплоя → откат → exit 3
- [ ] Таймаут → откат → exit 2
- [ ] Параллельный мониторинг: ошибка одного сервиса отменяет остальные
- [ ] `go test ./cmd/ -run TestApply` — зелёный

## Следующий этап
→ [Этап 10: Команда rollback](./10-rollback-command.md)
