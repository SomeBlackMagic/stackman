# Этап 12а — Команда `diff` (`cmd/diff.go`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 1: CLI-скелет (заглушка `ExecuteDiff`)
- [x] Этап 2: `compose.ParseFile`
- [x] Этап 4: `paths.NewResolver`
- [x] Этап 5б: `swarm.GetStackState`
- [x] Этап 8: `plan.CreatePlan`, `plan.FormatPlan`

## Что создаём
```
cmd/diff.go       # полная реализация (заменяет заглушку)
cmd/diff_test.go
```

---

## TDD-план

### Цикл 1: Отсутствие флагов → ошибка

**🔴 Тест:**
```go
// cmd/diff_test.go
package cmd_test

func TestDiff_MissingName(t *testing.T) {
    _, err := parseDiffArgs([]string{"-f", "compose.yml"})
    if err == nil || !strings.Contains(err.Error(), "--name") {
        t.Errorf("expected --name error, got %v", err)
    }
}

func TestDiff_MissingFile(t *testing.T) {
    _, err := parseDiffArgs([]string{"-n", "mystack"})
    if err == nil || !strings.Contains(err.Error(), "--compose-file") {
        t.Errorf("expected --compose-file error, got %v", err)
    }
}
```

**🟢 Реализация:**
```go
// cmd/diff.go
package cmd

type DiffArgs struct {
    StackName   string
    ComposeFile string
    Output      string // "text" или "json"
}

func parseDiffArgs(args []string) (*DiffArgs, error) {
    fs := flag.NewFlagSet("diff", flag.ContinueOnError)
    a := &DiffArgs{}
    fs.StringVar(&a.StackName, "name", "", "Stack name (required)")
    fs.StringVar(&a.StackName, "n", "", "Stack name (shorthand)")
    fs.StringVar(&a.ComposeFile, "compose-file", "", "Path to compose file (required)")
    fs.StringVar(&a.ComposeFile, "f", "", "Path to compose file (shorthand)")
    fs.StringVar(&a.Output, "output", "text", "Output format: text, json")

    if err := fs.Parse(args); err != nil {
        return nil, err
    }
    if a.StackName == "" {
        return nil, fmt.Errorf("required flag --name not set")
    }
    if a.ComposeFile == "" {
        return nil, fmt.Errorf("required flag --compose-file not set")
    }
    return a, nil
}
```

---

### Цикл 2: Нет изменений → "No changes"

**🔴 Тест:**
```go
func TestDiffWorkflow_NoChanges(t *testing.T) {
    mock := &swarm.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarmtypes.Service, error) {
            return []swarmtypes.Service{
                {Spec: swarmtypes.ServiceSpec{
                    Annotations: swarmtypes.Annotations{Name: "mystack_web"},
                    TaskTemplate: swarmtypes.TaskSpec{
                        ContainerSpec: &swarmtypes.ContainerSpec{Image: "nginx:latest"},
                    },
                }},
            }, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
    }

    tmpFile := writeTempCompose(t, `
version: "3.8"
services:
  web:
    image: nginx:latest
`)

    var stdout strings.Builder
    err := cmd.RunDiffWorkflow(context.Background(),
        &cmd.DiffArgs{StackName: "mystack", ComposeFile: tmpFile},
        cmd.DiffDeps{DockerClient: mock, Stdout: &stdout})
    if err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(stdout.String(), "No changes") {
        t.Errorf("expected 'No changes', got: %q", stdout.String())
    }
}
```

**🟢 Реализация `RunDiffWorkflow`:**
```go
// cmd/diff.go

type DiffDeps struct {
    DockerClient swarm.DockerClient
    Stdout       io.Writer
    Stderr       io.Writer
}

func RunDiffWorkflow(ctx context.Context, args *DiffArgs, deps DiffDeps) error {
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

    // 3. Текущее состояние
    current, err := swarm.GetStackState(ctx, deps.DockerClient, args.StackName)
    if err != nil {
        return fmt.Errorf("get stack state: %w", err)
    }

    // 4. Вычисляем план
    p, err := plan.CreatePlan(current, cf, args.StackName)
    if err != nil {
        return fmt.Errorf("create plan: %w", err)
    }

    // 5. Вывод
    switch args.Output {
    case "json":
        return outputPlanJSON(deps.Stdout, p)
    default:
        fmt.Fprint(deps.Stdout, plan.FormatPlan(p))
    }
    return nil
}

func outputPlanJSON(w io.Writer, p *plan.Plan) error {
    enc := json.NewEncoder(w)
    enc.SetIndent("", "  ")
    return enc.Encode(p)
}
```

---

### Цикл 3: Новый сервис → строка с "+"

**🔴 Тест:**
```go
func TestDiffWorkflow_NewService(t *testing.T) {
    mock := &swarm.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarmtypes.Service, error) {
            return nil, nil // пустой стек
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
    }

    tmpFile := writeTempCompose(t, `
version: "3.8"
services:
  api:
    image: myapi:1.0
`)

    var stdout strings.Builder
    cmd.RunDiffWorkflow(context.Background(),
        &cmd.DiffArgs{StackName: "mystack", ComposeFile: tmpFile},
        cmd.DiffDeps{DockerClient: mock, Stdout: &stdout})

    if !strings.Contains(stdout.String(), "+ api") {
        t.Errorf("expected '+ api' in output, got: %q", stdout.String())
    }
}
```

---

### Цикл 4: Удалённый сервис → строка с "-"

**🔴 Тест:**
```go
func TestDiffWorkflow_RemovedService(t *testing.T) {
    mock := &swarm.MockDockerClient{
        ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarmtypes.Service, error) {
            return []swarmtypes.Service{
                {ID: "svc1", Spec: swarmtypes.ServiceSpec{
                    Annotations: swarmtypes.Annotations{Name: "mystack_legacy"},
                }},
            }, nil
        },
        NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) { return nil, nil },
        VolumeListFn:  func(_ context.Context, _ filters.Args) (volume.ListResponse, error) { return volume.ListResponse{}, nil },
    }

    tmpFile := writeTempCompose(t, `version: "3.8"
services: {}`)

    var stdout strings.Builder
    cmd.RunDiffWorkflow(context.Background(),
        &cmd.DiffArgs{StackName: "mystack", ComposeFile: tmpFile},
        cmd.DiffDeps{DockerClient: mock, Stdout: &stdout})

    if !strings.Contains(stdout.String(), "- legacy") {
        t.Errorf("expected '- legacy' in output, got: %q", stdout.String())
    }
}
```

---

### Цикл 5: `--output json` → валидный JSON

**🔴 Тест:**
```go
func TestDiffWorkflow_JSONOutput(t *testing.T) {
    mock := newEmptyStackMock()
    tmpFile := writeTempCompose(t, `version: "3.8"
services:
  web:
    image: nginx:latest`)

    var stdout strings.Builder
    cmd.RunDiffWorkflow(context.Background(),
        &cmd.DiffArgs{StackName: "mystack", ComposeFile: tmpFile, Output: "json"},
        cmd.DiffDeps{DockerClient: mock, Stdout: &stdout})

    var result map[string]interface{}
    if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
        t.Errorf("expected valid JSON, got error: %v\noutput: %q", err, stdout.String())
    }
}
```

---

### Шаг 6: Заменить заглушку в `stubs.go`

Удалить `ExecuteDiff` из `cmd/stubs.go` и добавить полную реализацию в `cmd/diff.go`:

```go
func ExecuteDiff(args []string) {
    a, err := parseDiffArgs(args)
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

    if err := RunDiffWorkflow(context.Background(), a, DiffDeps{
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

- [ ] `stackman diff` без флагов → exit 1, понятная ошибка
- [ ] Идентичное состояние → "No changes." + exit 0
- [ ] Новый сервис → строка `+ <name>` + exit 0
- [ ] Удалённый сервис → строка `- <name>` + exit 0
- [ ] `--output json` → валидный JSON + exit 0
- [ ] `ExecuteDiff` больше не является заглушкой
- [ ] `go test ./cmd/ -run TestDiff` — зелёный

## Следующий этап
→ [Этап 12б: Команда status](./12b-status-command.md)
