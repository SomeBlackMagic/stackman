# Этап 1 — CLI-скелет (`cmd/`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 0: скелет проекта

## Что создаём
```
cmd/
├── root.go      # корневая команда, глобальные флаги
├── apply.go     # заглушка
├── rollback.go  # заглушка
├── logs.go      # заглушка
├── events.go    # заглушка
├── diff.go      # заглушка
├── status.go    # заглушка
├── version.go   # рабочая команда
└── root_test.go
main.go          # обновить: вызывать cmd.Execute()
```

---

## TDD-план

### Цикл 1: Базовая структура и помощь

**🔴 Тест:**
```go
// cmd/root_test.go
package cmd_test

import (
    "bytes"
    "testing"
)

func TestRootHelp(t *testing.T) {
    buf := &bytes.Buffer{}
    cmd := NewRootCommand()
    cmd.SetOut(buf)
    cmd.SetArgs([]string{"--help"})
    err := cmd.Execute()
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
    out := buf.String()
    for _, want := range []string{"apply", "rollback", "logs", "events", "diff", "status", "version"} {
        if !strings.Contains(out, want) {
            t.Errorf("--help output missing command %q", want)
        }
    }
}
```

**🟢 Реализация:**

`cmd/root.go`:
```go
package cmd

import (
    "os"
    "github.com/spf13/cobra"  // или стандартный flag — см. ниже
)

// Примечание: используем стандартный flag + ручная маршрутизация,
// без cobra, чтобы не добавлять зависимости.
// Если команда одна — cobra избыточна для MVP.
// Выбор: стандартный flag с подкомандами через os.Args[1].

func NewRootCommand() *RootCmd { ... }

func Execute() {
    if err := NewRootCommand().Execute(); err != nil {
        os.Exit(1)
    }
}
```

> **Примечание по выбору фреймворка:**
> Документ не накладывает требования на cobra vs стандартный flag.
> Рекомендуется стандартный `flag` + ручная диспетчеризация через `os.Args[1]`
> для минимизации внешних зависимостей. Если предпочтительна cobra — допустимо.
> Выбор фиксируется здесь и не меняется в дальнейших этапах.

**Минимальная реализация без cobra:**

```go
// cmd/root.go
package cmd

import (
    "fmt"
    "os"
)

var (
    Version   = "dev"
    Commit    = "unknown"
    BuildDate = "unknown"
)

func Execute() {
    if len(os.Args) < 2 {
        printHelp()
        os.Exit(0)
    }
    switch os.Args[1] {
    case "apply":
        ExecuteApply(os.Args[2:])
    case "rollback":
        ExecuteRollback(os.Args[2:])
    case "logs":
        ExecuteLogs(os.Args[2:])
    case "events":
        ExecuteEvents(os.Args[2:])
    case "diff":
        ExecuteDiff(os.Args[2:])
    case "status":
        ExecuteStatus(os.Args[2:])
    case "version":
        ExecuteVersion()
    case "--help", "-h", "help":
        printHelp()
    default:
        fmt.Fprintf(os.Stderr, "unknown command %q\n\nRun 'stackman --help' for usage.\n", os.Args[1])
        os.Exit(1)
    }
}

func printHelp() {
    fmt.Print(`Usage: stackman <command> [flags]

Commands:
  apply     Deploy a stack to Docker Swarm
  rollback  Roll back a stack to its previous state
  logs      Stream logs from stack services
  events    Show Docker events for a stack
  diff      Show changes between current state and compose file
  status    Show current status of stack services
  version   Print version information

Global flags:
  --log-level string   Log level: debug, info, warn, error (default "info")
  --no-color           Disable colored output
  --output string      Output format: text, json (default "text")

Run 'stackman <command> --help' for more information.
`)
}
```

---

### Цикл 2: Команда `version`

**🔴 Тест:**
```go
// cmd/version_test.go
func TestVersionCommand(t *testing.T) {
    // Подменяем переменные
    origVersion, origCommit, origDate := Version, Commit, BuildDate
    Version, Commit, BuildDate = "1.0.0", "abc1234", "2026-01-01T00:00:00Z"
    defer func() { Version, Commit, BuildDate = origVersion, origCommit, origDate }()

    buf := &bytes.Buffer{}
    ExecuteVersionTo(buf)
    out := buf.String()

    for _, want := range []string{"1.0.0", "abc1234", "2026-01-01T00:00:00Z"} {
        if !strings.Contains(out, want) {
            t.Errorf("version output missing %q, got:\n%s", want, out)
        }
    }
}
```

**🟢 Реализация:**
```go
// cmd/version.go
package cmd

import (
    "fmt"
    "io"
    "os"
)

func ExecuteVersion() {
    ExecuteVersionTo(os.Stdout)
}

func ExecuteVersionTo(w io.Writer) {
    fmt.Fprintf(w, "stackman %s\n  commit: %s\n  built:  %s\n", Version, Commit, BuildDate)
}
```

---

### Цикл 3: Заглушки с кодом выхода 1

**🔴 Тест:**
```go
// cmd/root_test.go
func TestStubCommandsExitOne(t *testing.T) {
    stubs := []string{"diff", "status"}
    for _, cmd := range stubs {
        t.Run(cmd, func(t *testing.T) {
            // Запускаем через subprocess чтобы поймать os.Exit
            if os.Getenv("TEST_SUBPROCESS") == "1" {
                os.Args = []string{"stackman", cmd}
                Execute()
                return
            }
            proc := exec.Command(os.Args[0], "-test.run=TestStubCommandsExitOne/"+cmd)
            proc.Env = append(os.Environ(), "TEST_SUBPROCESS=1")
            err := proc.Run()
            exitErr, ok := err.(*exec.ExitError)
            if !ok || exitErr.ExitCode() != 1 {
                t.Errorf("expected exit code 1, got %v", err)
            }
        })
    }
}
```

**🟢 Реализация:**
```go
// cmd/stubs.go
package cmd

import (
    "fmt"
    "os"
)

func notImplemented(name string) {
    fmt.Fprintf(os.Stderr, "%s: not implemented yet\n", name)
    os.Exit(1)
}

func ExecuteDiff(_ []string)   { notImplemented("diff") }
func ExecuteStatus(_ []string) { notImplemented("status") }
```

---

### Цикл 4: Флаги команды `apply`

**🔴 Тест:**
```go
// cmd/apply_test.go
func TestApplyRequiredFlags(t *testing.T) {
    // --name обязателен
    err := runApplyFlags([]string{"-f", "docker-compose.yml"})
    if err == nil || !strings.Contains(err.Error(), "--name") {
        t.Errorf("expected error about --name, got %v", err)
    }

    // --compose-file обязателен
    err = runApplyFlags([]string{"-n", "mystack"})
    if err == nil || !strings.Contains(err.Error(), "--compose-file") {
        t.Errorf("expected error about --compose-file, got %v", err)
    }
}

// runApplyFlags парсит флаги и возвращает ошибку валидации, не запускает деплой
func runApplyFlags(args []string) error {
    _, err := parseApplyArgs(args)
    return err
}
```

**🟢 Реализация:**
```go
// cmd/apply.go
package cmd

import (
    "flag"
    "fmt"
    "os"
)

type ApplyArgs struct {
    StackName   string
    ComposeFile string
    Timeout     string
    Prune       bool
    NoLogs      bool
    Output      string
    LogLevel    string
}

func parseApplyArgs(args []string) (*ApplyArgs, error) {
    fs := flag.NewFlagSet("apply", flag.ContinueOnError)
    a := &ApplyArgs{}
    fs.StringVar(&a.StackName, "name", "", "Stack name (required)")
    fs.StringVar(&a.StackName, "n", "", "Stack name (shorthand)")
    fs.StringVar(&a.ComposeFile, "compose-file", "", "Path to compose file (required)")
    fs.StringVar(&a.ComposeFile, "f", "", "Path to compose file (shorthand)")
    fs.StringVar(&a.Timeout, "timeout", "5m", "Deployment timeout")
    fs.BoolVar(&a.Prune, "prune", false, "Remove orphaned resources")
    fs.BoolVar(&a.NoLogs, "no-logs", false, "Disable log streaming")
    fs.StringVar(&a.Output, "output", "text", "Output format: text, json")
    fs.StringVar(&a.LogLevel, "log-level", "info", "Log level")

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

func ExecuteApply(args []string) {
    a, err := parseApplyArgs(args)
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    _ = a
    fmt.Fprintln(os.Stderr, "apply: not implemented yet")
    os.Exit(1)
}
```

---

### Шаг 5: Обновить `main.go`

```go
// main.go
package main

import "github.com/SomeBlackMagic/stackman/cmd"

// Переменные заполняются ldflags при сборке
var (
    Version   = "dev"
    Commit    = "unknown"
    BuildDate = "unknown"
)

func init() {
    cmd.Version   = Version
    cmd.Commit    = Commit
    cmd.BuildDate = BuildDate
}

func main() {
    cmd.Execute()
}
```

---

## Критерии завершения этапа

- [ ] `./stackman --help` показывает все 7 команд
- [ ] `./stackman version` выводит Version, Commit, BuildDate
- [ ] `./stackman apply` без флагов → stderr + exit 1
- [ ] `./stackman apply -n x` без `-f` → ошибка о `--compose-file`
- [ ] `./stackman diff` → "not implemented yet" + exit 1
- [ ] `./stackman status` → "not implemented yet" + exit 1
- [ ] `./stackman unknown` → "unknown command" + exit 1
- [ ] `go test ./cmd/...` проходит

## Следующий этап
→ [Этап 2: Парсинг Compose](./02-compose-parser.md)
