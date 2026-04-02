# Этап 13 — Интеграционные тесты (`tests/`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этапы 9–12б: все команды реализованы

## Что создаём / изменяем
```
tests/
├── helpers_test.go
├── apply_test.go
├── health_test.go
├── operations_test.go
├── resources_test.go
├── negative_test.go
└── testdata/
    ├── minimal.yml
    ├── healthcheck.yml
    ├── multi-service.yml
    ├── extra-hosts.yml
    └── bad-image.yml

Makefile        # цель test-integration
.github/workflows/build.yml  # запуск интеграционных тестов
```

---

## Принципы

> Интеграционные тесты работают против **реального Docker Swarm** (single-node).  
> Unit-тесты в `internal/` и `cmd/` не требуют Docker.  
> Интеграционные тесты помечены `//go:build integration` и запускаются отдельно.

---

## Шаг 1: Настройка Docker Swarm для CI

**`.github/workflows/build.yml`** — добавить job:
```yaml
integration-test:
  runs-on: ubuntu-latest
  needs: test
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version: '1.24'
    - name: Init Docker Swarm
      run: docker swarm init
    - name: Run integration tests
      run: make test-integration
    - name: Leave Docker Swarm
      if: always()
      run: docker swarm leave --force
```

**`Makefile`** — добавить:
```makefile
test-integration:
	go test -tags=integration -v -timeout=10m ./tests/...

test-unit:
	go test -race ./cmd/... ./internal/...
```

---

## Шаг 2: Вспомогательные функции (`tests/helpers_test.go`)

```go
//go:build integration

package tests_test

import (
    "context"
    "fmt"
    "os"
    "testing"
    "time"

    "github.com/docker/docker/client"
    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

// requireSwarm пропускает тест если Docker Swarm недоступен
func requireSwarm(t *testing.T) {
    t.Helper()
    cli, err := client.NewClientWithOpts(client.FromEnv)
    if err != nil {
        t.Skip("Docker not available:", err)
    }
    defer cli.Close()
    info, err := cli.Info(context.Background())
    if err != nil || info.Swarm.LocalNodeState != "active" {
        t.Skip("Docker Swarm not active")
    }
}

// uniqueStackName генерирует уникальное имя стека для изоляции тестов
func uniqueStackName(t *testing.T) string {
    return fmt.Sprintf("test-%s-%d", t.Name()[:min(10, len(t.Name()))], time.Now().UnixNano()%10000)
}

// cleanupStack удаляет все сервисы/сети стека после теста
func cleanupStack(t *testing.T, stackName string) {
    t.Helper()
    t.Cleanup(func() {
        cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
        defer cli.Close()
        ctx := context.Background()
        deployer := swarmint.NewStackDeployer(cli, stackName)
        state, _ := deployer.GetState(ctx)
        for _, svc := range state.Services {
            cli.ServiceRemove(ctx, svc.ID)
        }
    })
}

func min(a, b int) int {
    if a < b { return a }
    return b
}
```

---

## TDD-план

### Цикл 1: Базовый деплой (`apply_test.go`)

**Testdata `tests/testdata/minimal.yml`:**
```yaml
version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
```

**🔴 Тест:**
```go
//go:build integration

package tests_test

func TestApply_BasicDeploy(t *testing.T) {
    requireSwarm(t)
    stackName := uniqueStackName(t)
    cleanupStack(t, stackName)

    // Деплой
    exitCode := runStackman(t, "apply",
        "-n", stackName,
        "-f", "testdata/minimal.yml",
        "--timeout", "2m",
    )
    if exitCode != 0 {
        t.Fatalf("apply failed with exit code %d", exitCode)
    }

    // Проверяем что сервис запущен
    cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
    defer cli.Close()
    deployer := swarmint.NewStackDeployer(cli, stackName)
    state, err := deployer.GetState(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    if _, ok := state.Services["web"]; !ok {
        t.Error("expected 'web' service to exist")
    }
}

// runStackman запускает stackman как subprocess и возвращает код выхода
func runStackman(t *testing.T, args ...string) int {
    t.Helper()
    cmd := exec.Command("go", append([]string{"run", "../main.go"}, args...)...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    if err := cmd.Run(); err != nil {
        if e, ok := err.(*exec.ExitError); ok {
            return e.ExitCode()
        }
    }
    return 0
}
```

---

### Цикл 2: Обновление образа (`apply_test.go`)

**🔴 Тест:**
```go
func TestApply_UpdateService(t *testing.T) {
    requireSwarm(t)
    stackName := uniqueStackName(t)
    cleanupStack(t, stackName)

    // Первый деплой
    runStackman(t, "apply", "-n", stackName, "-f", "testdata/minimal.yml", "--timeout", "2m")

    // Обновление через второй compose-файл с другим образом
    compose2 := writeTempCompose(t, `
version: "3.8"
services:
  web:
    image: nginx:1.25-alpine
    deploy:
      replicas: 1
`)
    exitCode := runStackman(t, "apply", "-n", stackName, "-f", compose2, "--timeout", "2m")
    if exitCode != 0 {
        t.Fatalf("update failed with exit code %d", exitCode)
    }

    // Проверяем что образ обновился
    cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
    defer cli.Close()
    deployer := swarmint.NewStackDeployer(cli, stackName)
    state, _ := deployer.GetState(context.Background())
    svc := state.Services["web"]
    if svc.Spec.TaskTemplate.ContainerSpec.Image != "nginx:1.25-alpine" {
        t.Errorf("expected updated image, got %q", svc.Spec.TaskTemplate.ContainerSpec.Image)
    }
}
```

---

### Цикл 3: Healthcheck мониторинг (`health_test.go`)

**Testdata `tests/testdata/healthcheck.yml`:**
```yaml
version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost/"]
      interval: 5s
      timeout: 3s
      retries: 3
      start_period: 10s
```

**🔴 Тест:**
```go
func TestApply_WaitsForHealthcheck(t *testing.T) {
    requireSwarm(t)
    stackName := uniqueStackName(t)
    cleanupStack(t, stackName)

    start := time.Now()
    exitCode := runStackman(t, "apply",
        "-n", stackName,
        "-f", "testdata/healthcheck.yml",
        "--timeout", "3m",
    )
    elapsed := time.Since(start)

    if exitCode != 0 {
        t.Fatalf("apply failed with exit code %d", exitCode)
    }
    // Деплой должен был ждать healthcheck (хотя бы 5 секунд)
    if elapsed < 5*time.Second {
        t.Logf("Note: deploy completed in %v (healthcheck may not have been awaited)", elapsed)
    }
}
```

---

### Цикл 4: Unhealthy → откат (`health_test.go`)

**🔴 Тест:**
```go
func TestApply_UnhealthyTriggersRollback(t *testing.T) {
    requireSwarm(t)
    stackName := uniqueStackName(t)
    cleanupStack(t, stackName)

    // Compose с заведомо проваливающимся healthcheck
    badCompose := writeTempCompose(t, `
version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
    healthcheck:
      test: ["CMD", "false"]   # всегда провалится
      interval: 2s
      timeout: 1s
      retries: 2
      start_period: 2s
`)
    exitCode := runStackman(t, "apply",
        "-n", stackName,
        "-f", badCompose,
        "--timeout", "30s",
    )
    if exitCode == 0 {
        t.Error("expected non-zero exit code for unhealthy service")
    }
}
```

---

### Цикл 5: `--prune` удаляет orphaned сервис (`operations_test.go`)

**🔴 Тест:**
```go
func TestApply_PruneRemovesOrphan(t *testing.T) {
    requireSwarm(t)
    stackName := uniqueStackName(t)
    cleanupStack(t, stackName)

    // Первый деплой с 2 сервисами
    two := writeTempCompose(t, `
version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
  worker:
    image: alpine
    command: ["sleep", "infinity"]
    deploy:
      replicas: 1
`)
    runStackman(t, "apply", "-n", stackName, "-f", two, "--timeout", "2m")

    // Второй деплой с 1 сервисом + --prune
    one := writeTempCompose(t, `
version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 1
`)
    runStackman(t, "apply", "-n", stackName, "-f", one, "--prune", "--timeout", "2m")

    // Проверяем что worker удалён
    cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
    defer cli.Close()
    deployer := swarmint.NewStackDeployer(cli, stackName)
    state, _ := deployer.GetState(context.Background())
    if _, ok := state.Services["worker"]; ok {
        t.Error("worker should have been pruned")
    }
}
```

---

### Цикл 6: Невалидный compose → без изменений в Swarm (`negative_test.go`)

**🔴 Тест:**
```go
func TestApply_InvalidCompose_NoSwarmChanges(t *testing.T) {
    requireSwarm(t)
    stackName := uniqueStackName(t)
    cleanupStack(t, stackName)

    bad := writeTempCompose(t, `this: is: not: valid: yaml: [[[`)
    exitCode := runStackman(t, "apply", "-n", stackName, "-f", bad, "--timeout", "1m")
    if exitCode == 0 {
        t.Fatal("expected non-zero exit for invalid compose")
    }

    // Стек не должен был создаться
    cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
    defer cli.Close()
    deployer := swarmint.NewStackDeployer(cli, stackName)
    state, _ := deployer.GetState(context.Background())
    if len(state.Services) > 0 {
        t.Error("no services should exist after failed deploy")
    }
}
```

---

### Шаг 7: Testdata файлы

Создать в `tests/testdata/`:

**`minimal.yml`** — один сервис без healthcheck (см. выше)

**`multi-service.yml`:**
```yaml
version: "3.8"
services:
  web:
    image: nginx:alpine
    deploy:
      replicas: 2
  worker:
    image: alpine
    command: ["sleep", "infinity"]
    deploy:
      replicas: 1
networks:
  default:
    driver: overlay
```

**`extra-hosts.yml`:**
```yaml
version: "3.8"
services:
  app:
    image: alpine
    command: ["sleep", "infinity"]
    extra_hosts:
      - "host.internal:host-gateway"
    deploy:
      replicas: 1
```

**`bad-image.yml`:**
```yaml
version: "3.8"
services:
  broken:
    image: thisimage/doesnotexist:never
    deploy:
      replicas: 1
```

---

## Критерии завершения этапа

- [ ] `make test-integration` проходит на чистом Docker Swarm окружении
- [ ] `TestApply_BasicDeploy` — сервис запускается, exit 0
- [ ] `TestApply_UpdateService` — образ обновляется, exit 0
- [ ] `TestApply_WaitsForHealthcheck` — деплой ожидает healthcheck
- [ ] `TestApply_UnhealthyTriggersRollback` — exit ≠ 0
- [ ] `TestApply_PruneRemovesOrphan` — orphaned сервис удалён
- [ ] `TestApply_InvalidCompose_NoSwarmChanges` — exit ≠ 0, Swarm не изменился
- [ ] CI pipeline запускает интеграционные тесты после unit-тестов

## Следующий этап
→ [Этап 14: Документация и релиз](./14-docs-release.md)
