# Health Check Testing Guide

## Verification that stackman waits for healthcheck

This guide demonstrates that stackman correctly waits for services with healthchecks to become healthy before completing deployment.

## Test Setup

### Test Stack
File: `testdata/healthcheck-demo.yml`

```yaml
services:
  web:
    image: nginx:alpine
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost"]
      interval: 3s
      timeout: 2s
      retries: 3
      start_period: 10s  # Intentional delay to demonstrate waiting

  api:
    image: nginx:alpine
    # No healthcheck - should not block deployment
```

## Manual Test Steps

### 1. Deploy the stack
```bash
./stackman apply -n healthcheck-test -f testdata/healthcheck-demo.yml -timeout 3m
```

### 2. Observe the output

You should see:
```
Deploying stack: healthcheck-test
Stack deployed successfully.
Waiting for 2 service(s) to become healthy...
[ServiceUpdateMonitor] Waiting for service healthcheck-test_web update to complete...
[ServiceUpdateMonitor] Waiting for service healthcheck-test_api update to complete...
✅ Service healthcheck-test_web update completed
✅ Service healthcheck-test_api update completed
Waiting for: [healthcheck-test_web (health: starting)]
Waiting for: [healthcheck-test_web (health: starting)]
...
All services are healthy!
```

### 3. Verify timing

- The command should **NOT** exit immediately after deployment
- It should **wait** for the healthcheck to pass
- With `start_period: 10s`, expect ~10-15 seconds of waiting
- Service without healthcheck (api) should not block

### 4. Cleanup
```bash
docker stack rm healthcheck-test
```

## What is Being Tested

✅ **Service Update Completion**
- Waits for `UpdateStatus.State == "completed"` for each service

✅ **Task State Verification**
- Checks that tasks reach `running` state
- Skips tasks from old versions

✅ **Health Check Waiting**
- For services WITH healthcheck: waits for `State.Health.Status == "healthy"`
- For services WITHOUT healthcheck: only requires `running` state

✅ **Progress Logging**
- Shows which services are still unhealthy
- Reports health status (starting → healthy)

## Expected Behavior

### Service with Healthcheck
```
1. Deploy → create tasks
2. Tasks start → state: starting
3. Container starts
4. Wait start_period (10s)
5. Run healthcheck every interval (3s)
6. After retries succeed → healthy
7. stackman continues
```

### Service without Healthcheck
```
1. Deploy → create tasks
2. Tasks start → state: running
3. stackman considers it healthy immediately
```

## Testing with --no-wait Flag

To skip health checks:
```bash
./stackman apply -n test -f testdata/healthcheck-demo.yml --no-wait
```

This should exit immediately after deployment without waiting.

## Automated Verification

The integration test validates this behavior:
```bash
make test-integration
```

Look for:
```
Waiting for 2 service(s) to become healthy...
Waiting for: [stackman-test_nginx (health: starting)]
All services are healthy!
```

## Troubleshooting

### Command hangs forever
- Check healthcheck is actually passing: `docker service ps <service> --no-trunc`
- Check container logs: `docker service logs <service>`
- Increase timeout: `-timeout 5m`

### Command exits too quickly
- Verify healthcheck is defined in compose file
- Check logs for "Waiting for X service(s)"
- Use `-debug` flag (when implemented)

## Implementation Details

Health checking happens in two stages:

**Stage 1: Service Update Completion** (internal/health/service_update_monitor.go)
- Polls `ServiceInspect().UpdateStatus.State`
- Waits for state to become `"completed"`

**Stage 2: Task Health Verification** (cmd/apply.go:waitForAllTasksHealthy)
- Polls `TaskList()` for service tasks
- For each running task:
  - Gets container ID from `task.Status.ContainerStatus`
  - Inspects container: `ContainerInspect()`
  - Checks `container.State.Health.Status`
  - If no health defined, considers running as healthy

## Related Files
- `cmd/apply.go` - Main health check logic
- `internal/health/service_update_monitor.go` - Service update monitoring
- `testdata/simple-stack.yml` - Integration test stack with healthchecks

---

## Stackman и Docker API: управление Swarm как Helm управляет Kubernetes

### Концепция

Stackman управляет Docker Swarm-стеками через **Docker Engine API** — точно так же, как Helm управляет ресурсами Kubernetes через Kubernetes API.

```
Helm        →  values.yaml + Chart   →  Kubernetes API  →  Pods/Services/Deployments
Stackman    →  docker-compose.yml    →  Docker API       →  Swarm Services/Tasks/Containers
```

Оба инструмента реализуют **декларативную модель**: пользователь описывает желаемое состояние в файле конфигурации, а инструмент сам вычисляет разницу с текущим состоянием и применяет изменения через API.

### Как stackman использует Docker API

Вся работа со Swarm происходит через интерфейс `DockerClient` (`internal/swarm/interface.go`), который оборачивает Docker Engine HTTP API:

| Docker API вызов | Аналог kubectl/Helm | Назначение |
|---|---|---|
| `ServiceCreate` | `kubectl create deployment` | Создание нового сервиса |
| `ServiceUpdate` | `kubectl set image` / `helm upgrade` | Обновление существующего сервиса |
| `ServiceInspect` | `kubectl get deployment -o yaml` | Получение текущего состояния сервиса |
| `ServiceList` | `kubectl get deployments` | Список всех сервисов стека |
| `ServiceRemove` | `kubectl delete deployment` | Удаление сервиса |
| `TaskList` | `kubectl get pods` | Список запущенных задач (аналог Pods) |
| `ContainerInspect` | `kubectl describe pod` | Детальный статус контейнера и healthcheck |
| `NetworkCreate` / `NetworkRemove` | `kubectl apply -f network.yaml` | Управление overlay-сетями |
| `VolumeCreate` | `kubectl apply -f pvc.yaml` | Управление томами |

### Декларативный цикл управления

```
docker-compose.yml (желаемое состояние)
        │
        ▼
┌─────────────────────────────────┐
│  stackman apply                  │
│                                  │
│  1. Парсинг compose-файла        │  ← internal/compose
│  2. Снимок текущего состояния    │  ← internal/snapshot
│  3. Вычисление изменений (plan)  │  ← internal/plan
│  4. Применение через Docker API  │  ← internal/swarm
│  5. Ожидание сходимости          │  ← internal/health
└─────────────────────────────────┘
        │
        ▼
Docker Swarm (фактическое состояние)
  Services → Tasks → Containers
```

### Сравнение с Helm

| Возможность | Helm (Kubernetes) | Stackman (Docker Swarm) |
|---|---|---|
| Декларативный деплой | `helm install/upgrade` | `stackman apply` |
| Откат | `helm rollback` | `stackman rollback` (snapshot-based) |
| Ожидание готовности | `--wait` флаг | встроено по умолчанию |
| Healthcheck | Kubernetes readiness/liveness probes | Docker HEALTHCHECK |
| История релизов | Хранится в Secrets кластера | Snapshot в памяти (в процессе) |
| Шаблонизация | Go templates + values.yaml | В разработке (`--values` флаг) |
| Dry-run / diff | `helm diff` (plugin) | `internal/plan` (в разработке) |

### Механизм ожидания сходимости (convergence)

Это ключевое отличие от простого `docker stack deploy`. Stackman **ждёт**, пока Swarm фактически достигнет желаемого состояния:

```
docker stack deploy   →  отправил задание → вышел
stackman apply        →  отправил задание → ждёт сходимости → вышел с результатом
```

Сходимость определяется через два последовательных этапа (см. секцию [Implementation Details](#implementation-details) выше):
1. Swarm-оркестратор завершил rolling update сервиса
2. Все контейнеры нового поколения прошли healthcheck

Такой подход позволяет встраивать `stackman apply` в CI/CD пайплайны: ненулевой код возврата означает реальный сбой деплоя, а не просто ошибку отправки задания в Swarm.
