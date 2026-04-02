# Этап 12 — Команда `events` (`cmd/events.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 1: CLI-скелет
- [x] Этап 5а: `swarm.DockerClient`

## Что создаём
```
cmd/events.go
cmd/events_test.go
```

---

## TDD-план

### Цикл 1: Отсутствие --name → ошибка

**🔴 Тест:**
```go
// cmd/events_test.go
package cmd_test

func TestEvents_MissingName(t *testing.T) {
    _, err := parseEventsArgs([]string{})
    if err == nil || !strings.Contains(err.Error(), "--name") {
        t.Errorf("expected --name error, got %v", err)
    }
}
```

**🟢 Реализация:**
```go
// cmd/events.go
package cmd

type EventsArgs struct {
    StackName string
    Since     string
    Until     string
}

func parseEventsArgs(args []string) (*EventsArgs, error) {
    fs := flag.NewFlagSet("events", flag.ContinueOnError)
    a := &EventsArgs{}
    fs.StringVar(&a.StackName, "name", "", "Stack name (required)")
    fs.StringVar(&a.StackName, "n", "", "Stack name (shorthand)")
    fs.StringVar(&a.Since, "since", "", "Show events since timestamp (e.g. 5m)")
    fs.StringVar(&a.Until, "until", "", "Show events until timestamp")

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

### Цикл 2: События фильтруются по label стека

**🔴 Тест:**
```go
func TestEventsWorkflow_FiltersLabel(t *testing.T) {
    var capturedOpts dockerevents.ListOptions
    eventsCh := make(chan dockerevents.Message)
    errCh := make(chan error)

    mock := &swarm.MockDockerClient{
        EventsFn: func(_ context.Context, opts dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
            capturedOpts = opts
            return eventsCh, errCh
        },
    }

    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    cmd.RunEventsWorkflow(ctx,
        &cmd.EventsArgs{StackName: "mystack"},
        cmd.EventsDeps{DockerClient: mock, Stdout: io.Discard})

    // Проверяем что фильтр по label передан
    labels := capturedOpts.Filters.Get("label")
    found := false
    for _, l := range labels {
        if strings.Contains(l, "mystack") {
            found = true
        }
    }
    if !found {
        t.Errorf("expected label filter for 'mystack', got: %v", capturedOpts.Filters)
    }
}
```

**🟢 Реализация:**
```go
// cmd/events.go

type EventsDeps struct {
    DockerClient swarm.DockerClient
    Stdout       io.Writer
    Stderr       io.Writer
}

func RunEventsWorkflow(ctx context.Context, args *EventsArgs, deps EventsDeps) error {
    filterArgs := filters.NewArgs(
        filters.Arg("label", "com.docker.stack.namespace="+args.StackName),
    )

    opts := dockerevents.ListOptions{Filters: filterArgs}
    if args.Since != "" {
        opts.Since = args.Since
    }
    if args.Until != "" {
        opts.Until = args.Until
    }

    eventsCh, errCh := deps.DockerClient.Events(ctx, opts)

    for {
        select {
        case <-ctx.Done():
            return nil
        case err := <-errCh:
            if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
                return nil
            }
            return fmt.Errorf("events stream error: %w", err)
        case msg := <-eventsCh:
            formatEvent(deps.Stdout, msg)
        }
    }
}

func formatEvent(w io.Writer, msg dockerevents.Message) {
    ts := time.Unix(msg.Time, 0).Format("2006-01-02T15:04:05Z")
    actor := msg.Actor.ID
    if name, ok := msg.Actor.Attributes["name"]; ok {
        actor = name
    }
    fmt.Fprintf(w, "%s  %-12s  %-20s  %s\n", ts, msg.Type, actor, msg.Action)
}
```

---

### Цикл 3: Вывод содержит тип, актора и действие

**🔴 Тест:**
```go
func TestEventsWorkflow_FormatsOutput(t *testing.T) {
    eventsCh := make(chan dockerevents.Message, 1)
    errCh := make(chan error, 1)

    mock := &swarm.MockDockerClient{
        EventsFn: func(_ context.Context, _ dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error) {
            return eventsCh, errCh
        },
    }

    eventsCh <- dockerevents.Message{
        Type:   "service",
        Action: "update",
        Time:   time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).Unix(),
        Actor: dockerevents.Actor{
            ID: "svc1",
            Attributes: map[string]string{
                "name":                       "mystack_web",
                "com.docker.stack.namespace": "mystack",
            },
        },
    }
    close(eventsCh)

    var stdout strings.Builder
    ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
    defer cancel()

    cmd.RunEventsWorkflow(ctx,
        &cmd.EventsArgs{StackName: "mystack"},
        cmd.EventsDeps{DockerClient: mock, Stdout: &stdout})

    out := stdout.String()
    if !strings.Contains(out, "service") {
        t.Errorf("expected 'service' in output, got: %q", out)
    }
    if !strings.Contains(out, "update") {
        t.Errorf("expected 'update' in output, got: %q", out)
    }
    if !strings.Contains(out, "mystack_web") {
        t.Errorf("expected actor name in output, got: %q", out)
    }
}
```

---

## Критерии завершения этапа

- [ ] `stackman events` без `--name` → exit 1
- [ ] `Events()` вызывается с фильтром `label=com.docker.stack.namespace=<name>`
- [ ] Вывод строки: `timestamp  type  actor  action`
- [ ] `--since` / `--until` передаются в `ListOptions`
- [ ] Ctrl+C завершает с exit 0
- [ ] `go test ./cmd/ -run TestEvents` — зелёный

## Следующий этап
→ [Этап 12а: Команда diff](./12a-diff-command.md)
