# Этап 12б — Команда `status` (`cmd/status.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 1: CLI-скелет (заглушка `ExecuteStatus`)
- [x] Этап 5а: `swarm.DockerClient`
- [x] Этап 5б: `swarm.GetStackState`

## Что создаём
```
cmd/status.go       # полная реализация (заменяет заглушку)
cmd/status_test.go
```

---

## TDD-план

### Цикл 1: Отсутствие --name → ошибка

**🔴 Тест:**
```go
// cmd/status_test.go
package cmd_test

func TestStatus_MissingName(t *testing.T) {
    _, err := parseStatusArgs([]string{})
    if err == nil || !strings.Contains(err.Error(), "--name") {
        t.Errorf("expected --name error, got %v", err)
    }
}
```

**🟢 Реализация:**
```go
// cmd/status.go
package cmd

type StatusArgs struct {
    StackName string
    Output    string // "text" или "json"
}

func parseStatusArgs(args []string) (*StatusArgs, error) {
    fs := flag.NewFlagSet("status", flag.ContinueOnError)
    a := &StatusArgs{}
    fs.StringVar(&a.StackName, "name", "", "Stack name (required)")
    fs.StringVar(&a.StackName, "n", "", "Stack name (shorthand)")
    fs.StringVar(&a.Output, "output", "text", "Output format: text, json")

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
func TestStatusWorkflow_UnknownStack(t *testing.T) {
    mock := &swarm.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarmtypes.Service, error) { return nil, nil },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
    }
    err := cmd.RunStatusWorkflow(context.Background(),
        &cmd.StatusArgs{StackName: "ghost"},
        cmd.StatusDeps{DockerClient: mock, Stdout: io.Discard, Stderr: io.Discard})
    if err == nil {
        t.Fatal("expected error for nonexistent stack")
    }
}
```

---

### Цикл 3: Таблица с заголовками и данными

**🔴 Тест:**
```go
func TestStatusWorkflow_OutputsTable(t *testing.T) {
    replicas := uint64(3)
    mock := &swarm.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarmtypes.Service, error) {
            return []swarmtypes.Service{
                {
                    ID: "svc1",
                    Spec: swarmtypes.ServiceSpec{
                        Annotations: swarmtypes.Annotations{Name: "mystack_web"},
                        Mode: swarmtypes.ServiceMode{
                            Replicated: &swarmtypes.ReplicatedService{Replicas: &replicas},
                        },
                        TaskTemplate: swarmtypes.TaskSpec{
                            ContainerSpec: &swarmtypes.ContainerSpec{Image: "nginx:1.25"},
                        },
                    },
                },
            }, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
        TaskListFn: func(_ context.Context, _ types.TaskListOptions) ([]swarmtypes.Task, error) {
            return []swarmtypes.Task{
                {Status: swarmtypes.TaskStatus{State: swarmtypes.TaskStateRunning}},
                {Status: swarmtypes.TaskStatus{State: swarmtypes.TaskStateRunning}},
                {Status: swarmtypes.TaskStatus{State: swarmtypes.TaskStateRunning}},
            }, nil
        },
    }

    var stdout strings.Builder
    err := cmd.RunStatusWorkflow(context.Background(),
        &cmd.StatusArgs{StackName: "mystack"},
        cmd.StatusDeps{DockerClient: mock, Stdout: &stdout})
    if err != nil {
        t.Fatal(err)
    }
    out := stdout.String()

    // Заголовки
    for _, header := range []string{"NAME", "IMAGE", "REPLICAS", "STATUS"} {
        if !strings.Contains(out, header) {
            t.Errorf("expected header %q in output, got:\n%s", header, out)
        }
    }
    // Данные
    if !strings.Contains(out, "web") {
        t.Errorf("expected service name 'web', got:\n%s", out)
    }
    if !strings.Contains(out, "nginx:1.25") {
        t.Errorf("expected image 'nginx:1.25', got:\n%s", out)
    }
    if !strings.Contains(out, "3/3") {
        t.Errorf("expected '3/3' replicas, got:\n%s", out)
    }
}
```

**🟢 Реализация:**
```go
// cmd/status.go

type ServiceStatus struct {
    Name     string `json:"name"`
    Image    string `json:"image"`
    Replicas string `json:"replicas"` // "running/desired"
    Status   string `json:"status"`
}

type StatusDeps struct {
    DockerClient swarm.DockerClient
    Stdout       io.Writer
    Stderr       io.Writer
}

func RunStatusWorkflow(ctx context.Context, args *StatusArgs, deps StatusDeps) error {
    state, err := swarm.GetStackState(ctx, deps.DockerClient, args.StackName)
    if err != nil {
        return fmt.Errorf("get stack state: %w", err)
    }
    if len(state.Services) == 0 {
        return fmt.Errorf("stack %q not found or has no services", args.StackName)
    }

    statuses, err := collectStatuses(ctx, deps.DockerClient, state, args.StackName)
    if err != nil {
        return err
    }

    switch args.Output {
    case "json":
        enc := json.NewEncoder(deps.Stdout)
        enc.SetIndent("", "  ")
        return enc.Encode(statuses)
    default:
        printStatusTable(deps.Stdout, statuses)
    }
    return nil
}

func collectStatuses(ctx context.Context, client swarm.DockerClient, state *swarm.StackState, stackName string) ([]ServiceStatus, error) {
    var statuses []ServiceStatus

    for shortName, svc := range state.Services {
        desired := uint64(1)
        if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
            desired = *svc.Spec.Mode.Replicated.Replicas
        }

        // Считаем running задачи
        tasks, err := client.TaskList(ctx, types.TaskListOptions{
            Filters: filters.NewArgs(
                filters.Arg("service", svc.ID),
                filters.Arg("desired-state", "running"),
            ),
        })
        if err != nil {
            return nil, fmt.Errorf("list tasks for %q: %w", shortName, err)
        }

        running := uint64(0)
        hasFailures := false
        for _, t := range tasks {
            switch t.Status.State {
            case swarmtypes.TaskStateRunning:
                running++
            case swarmtypes.TaskStateFailed, swarmtypes.TaskStateRejected:
                hasFailures = true
            }
        }

        image := ""
        if svc.Spec.TaskTemplate.ContainerSpec != nil {
            image = svc.Spec.TaskTemplate.ContainerSpec.Image
        }

        status := deriveStatus(running, desired, hasFailures)

        statuses = append(statuses, ServiceStatus{
            Name:     shortName,
            Image:    image,
            Replicas: fmt.Sprintf("%d/%d", running, desired),
            Status:   status,
        })
    }

    // Сортируем по имени для стабильного вывода
    sort.Slice(statuses, func(i, j int) bool {
        return statuses[i].Name < statuses[j].Name
    })
    return statuses, nil
}

func deriveStatus(running, desired uint64, hasFailures bool) string {
    if hasFailures {
        return "failed"
    }
    if running == 0 {
        return "stopped"
    }
    if running < desired {
        return "partial"
    }
    return "running"
}

func printStatusTable(w io.Writer, statuses []ServiceStatus) {
    // Ширина колонок
    fmt.Fprintf(w, "%-30s  %-40s  %-10s  %s\n", "NAME", "IMAGE", "REPLICAS", "STATUS")
    fmt.Fprintf(w, "%s\n", strings.Repeat("-", 90))
    for _, s := range statuses {
        fmt.Fprintf(w, "%-30s  %-40s  %-10s  %s\n", s.Name, s.Image, s.Replicas, s.Status)
    }
}
```

---

### Цикл 4: `--output json` → массив объектов

**🔴 Тест:**
```go
func TestStatusWorkflow_JSONOutput(t *testing.T) {
    replicas := uint64(1)
    mock := &swarm.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarmtypes.Service, error) {
            return []swarmtypes.Service{
                {ID: "svc1", Spec: swarmtypes.ServiceSpec{
                    Annotations: swarmtypes.Annotations{Name: "mystack_api"},
                    Mode: swarmtypes.ServiceMode{Replicated: &swarmtypes.ReplicatedService{Replicas: &replicas}},
                    TaskTemplate: swarmtypes.TaskSpec{ContainerSpec: &swarmtypes.ContainerSpec{Image: "api:1"}},
                }},
            }, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
        TaskListFn: func(_ context.Context, _ types.TaskListOptions) ([]swarmtypes.Task, error) {
            return []swarmtypes.Task{
                {Status: swarmtypes.TaskStatus{State: swarmtypes.TaskStateRunning}},
            }, nil
        },
    }

    var stdout strings.Builder
    cmd.RunStatusWorkflow(context.Background(),
        &cmd.StatusArgs{StackName: "mystack", Output: "json"},
        cmd.StatusDeps{DockerClient: mock, Stdout: &stdout})

    var result []cmd.ServiceStatus
    if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
        t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
    }
    if len(result) != 1 {
        t.Fatalf("expected 1 service, got %d", len(result))
    }
    if result[0].Name != "api" {
        t.Errorf("expected name 'api', got %q", result[0].Name)
    }
    if result[0].Status != "running" {
        t.Errorf("expected status 'running', got %q", result[0].Status)
    }
}
```

---

### Шаг 5: Заменить заглушку в `stubs.go`

```go
func ExecuteStatus(args []string) {
    a, err := parseStatusArgs(args)
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

    if err := RunStatusWorkflow(context.Background(), a, StatusDeps{
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

- [ ] `stackman status` без `--name` → exit 1
- [ ] Несуществующий стек → ошибка + exit 1
- [ ] Таблица содержит заголовки: NAME, IMAGE, REPLICAS, STATUS
- [ ] `REPLICAS` показывает `running/desired`
- [ ] Статус: `running` / `partial` / `failed` / `stopped`
- [ ] `--output json` → валидный JSON-массив с полями `name`, `image`, `replicas`, `status`
- [ ] `ExecuteStatus` больше не является заглушкой
- [ ] `go test ./cmd/ -run TestStatus` — зелёный

## Следующий этап
→ [Этап 13: Интеграционные тесты](./13-integration-tests.md)
