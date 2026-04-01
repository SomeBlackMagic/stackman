# Этап 4 — Утилиты путей (`internal/paths/`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 0: скелет проекта

## Что создаём
```
internal/paths/
├── resolver.go
└── resolver_test.go
```

---

## TDD-план

### Цикл 1: Абсолютный путь — без изменений

**🔴 Тест:**
```go
// internal/paths/resolver_test.go
package paths_test

import (
    "testing"
    "github.com/SomeBlackMagic/stackman/internal/paths"
)

func TestResolve_AbsolutePath(t *testing.T) {
    r, err := paths.NewResolver()
    if err != nil {
        t.Fatal(err)
    }
    got := r.Resolve("/etc/docker/compose.yml")
    if got != "/etc/docker/compose.yml" {
        t.Errorf("absolute path should be unchanged, got %q", got)
    }
}
```

**🟢 Реализация:**
```go
// internal/paths/resolver.go
package paths

import (
    "os"
    "path/filepath"
)

type Resolver struct {
    basePath string
}

func NewResolver() (*Resolver, error) {
    cwd, err := os.Getwd()
    if err != nil {
        return nil, err
    }
    return &Resolver{basePath: cwd}, nil
}

func (r *Resolver) BasePath() string { return r.basePath }

func (r *Resolver) Resolve(source string) string {
    if filepath.IsAbs(source) {
        return source
    }
    return source // TODO: остальные случаи
}
```

---

### Цикл 2: Относительный путь — резолвинг от basePath

**🔴 Тест:**
```go
func TestResolve_RelativePath(t *testing.T) {
    r := &paths.ResolverWithBase{Base: "/project"}
    got := r.Resolve("./stacks/app.yml")
    want := "/project/stacks/app.yml"
    if got != want {
        t.Errorf("got %q, want %q", got, want)
    }
}

func TestResolve_RelativeNoPrefix(t *testing.T) {
    r := &paths.ResolverWithBase{Base: "/project"}
    got := r.Resolve("app.yml")
    want := "/project/app.yml"
    if got != want {
        t.Errorf("got %q, want %q", got, want)
    }
}
```

> **Примечание:** Экспортируем `ResolverWithBase` для тестов, чтобы не зависеть от реального CWD.

**🟢 Обновить `resolver.go`:**
```go
// ResolverWithBase — экспортированный вариант для тестирования.
type ResolverWithBase struct {
    Base string
}

func (r *ResolverWithBase) Resolve(source string) string {
    return resolve(source, r.Base)
}

func (r *Resolver) Resolve(source string) string {
    return resolve(source, r.basePath)
}

func resolve(source, base string) string {
    if filepath.IsAbs(source) {
        return source
    }
    if isSpecialProtocol(source) {
        return source
    }
    expanded := expandTilde(source)
    if filepath.IsAbs(expanded) {
        return expanded
    }
    return filepath.Clean(filepath.Join(base, expanded))
}
```

---

### Цикл 3: Тильда `~`

**🔴 Тест:**
```go
func TestResolve_Tilde(t *testing.T) {
    home, _ := os.UserHomeDir()
    r := &paths.ResolverWithBase{Base: "/project"}
    got := r.Resolve("~/stacks/app.yml")
    want := filepath.Join(home, "stacks/app.yml")
    if got != want {
        t.Errorf("got %q, want %q", got, want)
    }
}
```

**🟢 Добавить:**
```go
func expandTilde(path string) string {
    if path == "~" || strings.HasPrefix(path, "~/") {
        home, err := os.UserHomeDir()
        if err != nil {
            return path
        }
        return filepath.Join(home, path[1:])
    }
    return path
}
```

---

### Цикл 4: Специальные протоколы — без изменений

**🔴 Тест:**
```go
func TestResolve_SpecialProtocols(t *testing.T) {
    r := &paths.ResolverWithBase{Base: "/project"}
    cases := []string{
        "nfs://host/path",
        "cifs://host/share",
        "tmpfs",
    }
    for _, c := range cases {
        got := r.Resolve(c)
        if got != c {
            t.Errorf("special path %q should be unchanged, got %q", c, got)
        }
    }
}
```

**🟢 Добавить:**
```go
var specialProtocols = []string{"nfs://", "cifs://", "tmpfs"}

func isSpecialProtocol(path string) bool {
    for _, p := range specialProtocols {
        if strings.HasPrefix(path, p) || path == p {
            return true
        }
    }
    return false
}
```

---

## Критерии завершения этапа

- [ ] Абсолютный путь → без изменений
- [ ] `./relative` → `basePath/relative`
- [ ] `relative` → `basePath/relative`
- [ ] `~/path` → `$HOME/path`
- [ ] `nfs://...`, `cifs://...` → без изменений
- [ ] `go test ./internal/paths/...` — зелёный

## Следующий этап
→ [Этап 5а: Docker Client Abstraction](./05a-docker-client-abstraction.md)
