# Этап 10 — Команда `rollback` (`cmd/rollback.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 1: CLI-скелет
- [x] Этап 5е: `swarm.StackDeployer`
- [x] Этап 6: `snapshot.StackSnapshot`

## Что создаём
```
cmd/rollback.go
cmd/rollback_test.go
```

---

## TDD-план

### Цикл 1: Отсутствие --name → ошибка

**🔴 Тест:**
```go
// cmd/rollback_test.go
package cmd_test

func TestRollback_MissingName(t *testing.T) {
    _, err := parseRollbackArgs([]string{})
    if err == nil || !strings.Contains(err.Error(), "--name") {
        t.Errorf("expected --name error, got %v", err)
    }
}
```

**🟢 Реализация:**
```go
// cmd/rollback.go
package cmd

import (
    "flag"
    "fmt"
)

type RollbackArgs struct {
    StackName string
    Timeout   string
}

func parseRollbackArgs(args []string) (*RollbackArgs, error) {
    fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
    a := &RollbackArgs{}
    fs.StringVar(&a.StackName, "name", "", "Stack name (required)")
    fs.StringVar(&a.StackName, "n", "", "Stack name (shorthand)")
    fs.StringVar(&a.Timeout, "timeout", "5m", "Rollback timeout")

    if err := fs.Parse(args); err != nil {
        return nil, err
    }
    if a.StackName == "" {
        return nil, fmt.Errorf("required flag --name not set")
    }
    return a, nil
}
```

---

### Цикл 2: Несуществующий стек → ошибка exit 1

**🔴 Тест:**
```go
func TestRollbackWorkflow_UnknownStack(t *testing.T) {
    mock := &swarm.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarmtypes.Service, error) {
            return nil, nil // пустой стек
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
    }

    var stderr strings.Builder
    err := cmd.RunRollbackWorkflow(context.Background(), &cmd.RollbackArgs{
        StackName: "nonexistent",
        Timeout:   "30s",
    }, cmd.RollbackDeps{DockerClient: mock, Stderr: &stderr})

    if err == nil {
        t.Fatal("expected error for nonexistent stack")
    }
    if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no services") {
        t.Errorf("expected 'not found' in error, got: %v", err)
    }
}
```

**🟢 Реализация `RunRollbackWorkflow`:**
```go
// cmd/rollback.go

type RollbackDeps struct {
    DockerClient swarm.DockerClient
    Stdout       io.Writer
    Stderr       io.Writer
}

func RunRollbackWorkflow(ctx context.Context, args *RollbackArgs, deps RollbackDeps) error {
    timeout, err := time.ParseDuration(args.Timeout)
    if err != nil {
        return fmt.Errorf("invalid timeout: %w", err)
    }
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    deployer := swarm.NewStackDeployer(deps.DockerClient, args.StackName)

    // Проверяем существование стека
    state, err := deployer.GetState(ctx)
    if err != nil {
        return fmt.Errorf("get stack state: %w", err)
    }
    if len(state.Services) == 0 {
        return fmt.Errorf("stack %q not found or has no services", args.StackName)
    }

    fmt.Fprintf(deps.Stdout, "[INFO] Rolling back stack %q (%d services)...\n",
        args.StackName, len(state.Services))

    // Используем Docker Swarm UpdateRollback API для каждого сервиса
    for name, svc := range state.Services {
        svcCopy := svc
        fmt.Fprintf(deps.Stdout, "[INFO] Rolling back service %q...\n", name)
        resp, err := deps.DockerClient.ServiceUpdate(ctx, svcCopy.ID, svcCopy.Version,
            svcCopy.Spec, types.ServiceUpdateOptions{Rollback: "previous"})
        if err != nil {
            return fmt.Errorf("rollback service %q: %w", name, err)
        }
        if len(resp.Warnings) > 0 {
            for _, w := range resp.Warnings {
                fmt.Fprintf(deps.Stderr, "[WARN] %s: %s\n", name, w)
            }
        }
    }

    fmt.Fprintf(deps.Stdout, "[INFO] Rollback of stack %q completed\n", args.StackName)
    return nil
}
```

---

### Цикл 3: Успешный откат → вызывает ServiceUpdate с Rollback

**🔴 Тест:**
```go
func TestRollbackWorkflow_Success(t *testing.T) {
    updateCalled := 0
    mock := &swarm.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarmtypes.Service, error) {
            return []swarmtypes.Service{
                {ID: "svc1", Version: swarmtypes.Version{Index: 10},
                    Spec: swarmtypes.ServiceSpec{Annotations: swarmtypes.Annotations{Name: "mystack_web"}}},
            }, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
        ServiceUpdateFn: func(_ context.Context, id string, _ swarmtypes.Version, _ swarmtypes.ServiceSpec, opts types.ServiceUpdateOptions) (types.ServiceUpdateResponse, error) {
            updateCalled++
            if opts.Rollback != "previous" {
                t.Errorf("expected Rollback=previous, got %q", opts.Rollback)
            }
            return types.ServiceUpdateResponse{}, nil
        },
    }

    var stdout strings.Builder
    err := cmd.RunRollbackWorkflow(context.Background(),
        &cmd.RollbackArgs{StackName: "mystack", Timeout: "30s"},
        cmd.RollbackDeps{DockerClient: mock, Stdout: &stdout, Stderr: io.Discard})

    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if updateCalled != 1 {
        t.Errorf("expected 1 ServiceUpdate call, got %d", updateCalled)
    }
    if !strings.Contains(stdout.String(), "completed") {
        t.Error("expected 'completed' in output")
    }
}
```

---

### Шаг 4: `ExecuteRollback` — финальная склейка

```go
func ExecuteRollback(args []string) {
    a, err := parseRollbackArgs(args)
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
    if err != nil {
        fmt.Fprintf(os.Stderr, "[ERROR] Docker connection failed: %v\n", err)
        os.Exit(1)
    }
    defer dockerClient.Close()

    ctx, cancel := context.WithCancel(context.Background())
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
    go func() { <-sigCh; cancel() }()

    if err := RunRollbackWorkflow(ctx, a, RollbackDeps{
        DockerClient: dockerClient,
        Stdout:       os.Stdout,
        Stderr:       os.Stderr,
    }); err != nil {
        fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
        os.Exit(1)
    }
}
```

---

## Критерии завершения этапа

- [ ] `stackman rollback` без `--name` → exit 1
- [ ] `stackman rollback -n nonexistent` → exit 1, "not found"
- [ ] Успешный откат → `ServiceUpdate` с `Rollback="previous"` + exit 0
- [ ] `--timeout` прерывает ожидание → exit 1 (context.DeadlineExceeded)
- [ ] `go test ./cmd/ -run TestRollback` — зелёный

## Следующий этап
→ [Этап 11: Команда logs](./11-logs-command.md)
