# Этап 11 — Команда `logs` (`cmd/logs.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 1: CLI-скелет
- [x] Этап 5а: `swarm.DockerClient`
- [x] Этап 5б: `swarm.GetStackState`

## Что создаём
```
cmd/logs.go
cmd/logs_test.go
```

---

## TDD-план

### Цикл 1: Отсутствие --name → ошибка

**🔴 Тест:**
```go
// cmd/logs_test.go
package cmd_test

func TestLogs_MissingName(t *testing.T) {
    _, err := parseLogsArgs([]string{})
    if err == nil || !strings.Contains(err.Error(), "--name") {
        t.Errorf("expected --name error, got %v", err)
    }
}
```

**🟢 Реализация:**
```go
// cmd/logs.go
package cmd

import (
    "flag"
    "fmt"
)

type LogsArgs struct {
    StackName    string
    Tail         string
    Follow       bool
    Since        string
    NoLogPrefix  bool
    Timestamps   bool
}

func parseLogsArgs(args []string) (*LogsArgs, error) {
    fs := flag.NewFlagSet("logs", flag.ContinueOnError)
    a := &LogsArgs{}
    fs.StringVar(&a.StackName, "name", "", "Stack name (required)")
    fs.StringVar(&a.StackName, "n", "", "Stack name (shorthand)")
    fs.StringVar(&a.Tail, "tail", "100", "Number of lines to show from end of logs")
    fs.BoolVar(&a.Follow, "follow", false, "Follow log output")
    fs.BoolVar(&a.Follow, "f", false, "Follow (shorthand)")
    fs.StringVar(&a.Since, "since", "", "Show logs since timestamp (e.g. 5m, 2006-01-02)")
    fs.BoolVar(&a.NoLogPrefix, "no-log-prefix", false, "Disable service name prefix")
    fs.BoolVar(&a.Timestamps, "timestamps", false, "Show timestamps")

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

### Цикл 2: Стек без сервисов → сообщение, exit 0

**🔴 Тест:**
```go
func TestLogsWorkflow_EmptyStack(t *testing.T) {
    mock := &swarm.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarmtypes.Service, error) {
            return nil, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
    }
    var stderr strings.Builder
    err := cmd.RunLogsWorkflow(context.Background(),
        &cmd.LogsArgs{StackName: "empty", Tail: "100"},
        cmd.LogsDeps{DockerClient: mock, Stdout: io.Discard, Stderr: &stderr})
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if !strings.Contains(stderr.String(), "no services") {
        t.Error("expected 'no services' warning")
    }
}
```

---

### Цикл 3: Стриминг логов с префиксом сервиса

**🔴 Тест:**
```go
func TestLogsWorkflow_OutputsPrefix(t *testing.T) {
    mock := &swarm.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarmtypes.Service, error) {
            return []swarmtypes.Service{
                {ID: "svc1", Spec: swarmtypes.ServiceSpec{
                    Annotations: swarmtypes.Annotations{Name: "mystack_web"},
                }},
            }, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
        ContainerLogsFn: func(_ context.Context, _ string, _ container.LogsOptions) (io.ReadCloser, error) {
            // Docker logs stream: 8-byte header + data
            line := "Hello from web\n"
            header := make([]byte, 8)
            header[0] = 1 // stdout
            binary.BigEndian.PutUint32(header[4:], uint32(len(line)))
            return io.NopCloser(bytes.NewReader(append(header, []byte(line)...))), nil
        },
        TaskListFn: func(_ context.Context, _ types.TaskListOptions) ([]swarmtypes.Task, error) {
            return []swarmtypes.Task{
                {ID: "task1", Status: swarmtypes.TaskStatus{
                    State: swarmtypes.TaskStateRunning,
                    ContainerStatus: &swarmtypes.ContainerStatus{ContainerID: "ctr1"},
                }},
            }, nil
        },
    }

    var stdout strings.Builder
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    err := cmd.RunLogsWorkflow(ctx,
        &cmd.LogsArgs{StackName: "mystack", Tail: "10", Follow: false},
        cmd.LogsDeps{DockerClient: mock, Stdout: &stdout, Stderr: io.Discard})

    if err != nil && !errors.Is(err, context.DeadlineExceeded) {
        t.Fatalf("unexpected error: %v", err)
    }
    out := stdout.String()
    if !strings.Contains(out, "web") {
        t.Errorf("expected service name 'web' as prefix in output, got: %q", out)
    }
    if !strings.Contains(out, "Hello from web") {
        t.Errorf("expected log line in output, got: %q", out)
    }
}
```

**🟢 Реализация `RunLogsWorkflow`:**
```go
// cmd/logs.go

type LogsDeps struct {
    DockerClient swarm.DockerClient
    Stdout       io.Writer
    Stderr       io.Writer
}

func RunLogsWorkflow(ctx context.Context, args *LogsArgs, deps LogsDeps) error {
    state, err := swarm.GetStackState(ctx, deps.DockerClient, args.StackName)
    if err != nil {
        return fmt.Errorf("get stack state: %w", err)
    }
    if len(state.Services) == 0 {
        fmt.Fprintf(deps.Stderr, "[WARN] Stack %q has no services\n", args.StackName)
        return nil
    }

    var wg sync.WaitGroup
    for shortName, svc := range state.Services {
        shortName, svc := shortName, svc
        wg.Add(1)
        go func() {
            defer wg.Done()
            if err := streamServiceLogs(ctx, deps.DockerClient, svc.ID, shortName, args, deps.Stdout); err != nil {
                if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
                    fmt.Fprintf(deps.Stderr, "[WARN] logs for %q: %v\n", shortName, err)
                }
            }
        }()
    }
    wg.Wait()
    return nil
}

func streamServiceLogs(ctx context.Context, client swarm.DockerClient, serviceID, serviceName string, args *LogsArgs, out io.Writer) error {
    // Получаем задачи сервиса для нахождения контейнеров
    tasks, err := client.TaskList(ctx, types.TaskListOptions{
        Filters: filters.NewArgs(
            filters.Arg("service", serviceID),
            filters.Arg("desired-state", "running"),
        ),
    })
    if err != nil {
        return fmt.Errorf("list tasks: %w", err)
    }

    for _, task := range tasks {
        if task.Status.ContainerStatus == nil || task.Status.ContainerStatus.ContainerID == "" {
            continue
        }
        rc, err := client.ContainerLogs(ctx, task.Status.ContainerStatus.ContainerID, container.LogsOptions{
            ShowStdout: true,
            ShowStderr: true,
            Follow:     args.Follow,
            Tail:       args.Tail,
            Timestamps: args.Timestamps,
        })
        if err != nil {
            return fmt.Errorf("get container logs: %w", err)
        }
        defer rc.Close()

        prefix := ""
        if !args.NoLogPrefix {
            prefix = fmt.Sprintf("[%s] ", serviceName)
        }
        if err := copyLogsWithPrefix(rc, out, prefix); err != nil {
            return err
        }
    }
    return nil
}

// copyLogsWithPrefix читает docker multiplexed log stream и добавляет префикс к каждой строке.
func copyLogsWithPrefix(src io.Reader, dst io.Writer, prefix string) error {
    // Docker log stream: 8-byte header (1 byte stream type, 3 bytes padding, 4 bytes size) + data
    hdr := make([]byte, 8)
    for {
        if _, err := io.ReadFull(src, hdr); err != nil {
            if errors.Is(err, io.EOF) {
                return nil
            }
            return err
        }
        size := binary.BigEndian.Uint32(hdr[4:])
        buf := make([]byte, size)
        if _, err := io.ReadFull(src, buf); err != nil {
            return err
        }
        lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
        for _, line := range lines {
            if line != "" {
                fmt.Fprintf(dst, "%s%s\n", prefix, line)
            }
        }
    }
}
```

---

### Цикл 4: `--no-log-prefix` убирает префикс

**🔴 Тест:**
```go
func TestLogsWorkflow_NoPrefix(t *testing.T) {
    // Аналогичен TestLogsWorkflow_OutputsPrefix но с NoLogPrefix: true
    // Проверяем что "[web]" НЕ присутствует в выводе
    ...
    if strings.Contains(out, "[web]") {
        t.Error("prefix should be absent with --no-log-prefix")
    }
}
```

---

## Критерии завершения этапа

- [ ] `stackman logs` без `--name` → exit 1
- [ ] Пустой стек → предупреждение, exit 0
- [ ] Каждая строка лога имеет префикс `[service-name]`
- [ ] `--no-log-prefix` убирает префикс
- [ ] `--follow` держит соединение до отмены контекста
- [ ] `--tail N` передаётся в `ContainerLogs`
- [ ] `go test ./cmd/ -run TestLogs` — зелёный

## Следующий этап
→ [Этап 12: Команда events](./12-events-command.md)
