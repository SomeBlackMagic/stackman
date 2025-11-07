# ТЗ: CLI-утилита «swarm-helm» для Docker Swarm

Основание: ввод пользователя + публичные возможности Docker Engine/Swarm API (без внешних источников). Ограничения и зависимости указаны ниже.

---

## 1. Цель
Консольная утилита на Go, аналог Helm для Kubernetes, но для Docker Swarm:
- читает `docker-compose.yml` (парсинг собственными средствами, без внешних Compose-библиотек);
- формирует желаемое состояние стека и применяет его через Docker Socket API;
- выполняет health-контроль релиза по событиям задач и состоянию контейнеров;
- поддерживает откат с ожиданием завершения;
- корректно реагирует на SIGINT/SIGTERM: прекращает ожидание и инициирует откат.

## 2. Жёсткие ограничения
- **Зависимости только:**
  ```
  github.com/docker/docker v28.5.1+incompatible
  github.com/docker/go-units v0.5.0
  golang.org/x/net v0.43.0
  gopkg.in/yaml.v3 v3.0.1
  ```
- **Запрещены подпроцессы.** Никакого `exec.Command`. Вся работа через Docker Socket API (Unix/TCP).
- **Парсинг docker-compose.yml** — средствами утилиты и `gopkg.in/yaml.v3`.
- **Авторизация к Registry:** переменная окружения `DOCKER_CONFIG_PATH` — каталог с `config.json` (формат Docker CLI) для pull образов (передаётся в поля `AuthConfig` клиента).
- **Конвертация путей для volume/монтирования:** относительные пути → абсолютные на базе `SWARM_STACK_PATH` или текущей директории.

Пример конвертации:
```go
// cwd := basePathFromEnvOrWD(SWARM_STACK_PATH)
if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
    source = strings.Replace(source, "./", cwd+"/", 1)
    if strings.HasPrefix(parts[0], "../") {
        source = cwd + "/" + parts[0]
    }
} else if !strings.HasPrefix(source, "/") && !strings.Contains(source, "://") {
    source = cwd + "/" + source
}
```

## 3. Поддерживаемые сущности и маппинг Compose → Swarm
- Сервисы: `services.*` → `swarm.ServiceSpec`:
  - `image`, `command`, `args`, `environment`, `env_file` (слияние), `labels`,
  - `deploy.replicas`, `deploy.update_config.{parallelism,delay,order,monitor,max_failure_ratio}`, `deploy.restart_policy.{condition,delay,max_attempts,window}`,
  - `deploy.placement.constraints` → `TaskTemplate.Placement.Constraints`,
  - `ports` → `EndpointSpec.Ports`,
  - `networks` → привязки к overlay-сетям,
  - `volumes` → `Mounts` (type `bind`/`volume`), преобразование путей,
  - `healthcheck` → `ContainerSpec.Healthcheck`,
  - `secrets`, `configs` → `ContainerSpec.Secrets/Configs` с `File` target,
  - `resources.limits/reservations` → `ResourceRequirements` (по минимуму, если возможно).
- Ресурсы: `networks` (overlay), `volumes` (local), `secrets`, `configs`.
- Объект “stack” отсутствует в API. Используем метку `com.docker.stack.namespace=<STACK>`.

Ограничения первого релиза:
- Не поддерживаем `build`, `profiles`, `x-*`. `depends_on: condition` не применяется на уровне Swarm.
- `volumes.external`, `networks.external` — только привязка без управления жизненным циклом.
- `tmpfs`, `sysctls` — опционально, если требуется, но не обязательны в MVP.

## 4. Формат входа
- `-f docker-compose.yml` (обязательно).
- `--values values.yaml` и `--set key=val` — примитивная шаблонизация: env‑подстановка `${VAR}` + replace по `values` и `--set`. Без внешних шаблонных либ.
- Переменные окружения доступны для `${VAR}`.

## 5. План применения (diff) и порядок операций
1. Считать текущее состояние: сервисы/сети/секреты/конфиги/волюмы с меткой стека.
2. Построить diff: **Create/Update/Delete**.
3. Применение в порядке зависимостей:
   - networks → configs/secrets → services → garbage‑collect (при `--prune` удаляем неиспользуемые).
4. Обновление сервиса: `ServiceUpdate` с актуальным `Version.Index`, маппинг всех изменившихся полей.

## 6. Healthy‑проверки релиза
Критерий для каждого затронутого сервиса:
- `ServiceInspect().UpdateStatus.State == "completed"` (после обновления) **и**
- все активные контейнеры задач имеют `State.Health.Status == healthy` (если healthcheck задан).
- Для новых сервисов, где UpdateStatus может быть пуст, критерий: нужное число реплик в `running` и health=healthy либо health отсутствует.

Механика:
- Подписка на `Events` с фильтрами: `type=task`, `event=create|update|start|die|destroy`, `label=com.docker.stack.namespace=<STACK>`.
- При создании задачи:
  - запуск горутины‑watcher:
    - `ContainerLogs(follow=true)` stdout/stderr;
    - `Events(type=container, id=<containerID>)`;
    - периодический `ContainerInspect` для `State.Health`.
  - закрытие watcher при конечных состояниях задачи: `complete|failed|rejected|shutdown|orphaned|remove`.
- Агрегатор по сервису ведёт:
  - прогресс реплик Ready/Desired;
  - карту `containerID -> health {healthy|unhealthy|none}`;
  - последнее `UpdateStatus` сервиса.

Тайминги (по умолчанию, переопределяются флагами/ENV):
- `--timeout` (или `SWARM_HELM_TIMEOUT`) релиза: 15m.
- `--task-health-timeout`: 5m. Если в compose есть `healthcheck.start_period/interval/retries`, использовать их как базовую логику ожидания.
- Интервал инспекта: 2s.

## 7. Алгоритм apply (псевдокод)
```go
ctx := withSignalsAndTimeout(...)

desired := parseCompose(render(composeFile, values, set, env))
current := readSwarmState(stackLabel)

snapshot := takeSnapshot(current, stackLabel)

plan := diff(current, desired)
if plan.Empty() && !noWait {
    // Нечего применять, просто подтвердить стабильность
    ok := waitHealthy(ctx, stack, timeout, services=current.Services)
    if ok { os.Exit(0) } else { os.Exit(2) }
}

if err := applyResources(ctx, plan.Resources); err != nil {
    _ = rollback(ctx, snapshot, rbTimeout)
    os.Exit(3)
}

if err := applyServices(ctx, plan.Services); err != nil {
    _ = rollback(ctx, snapshot, rbTimeout)
    os.Exit(3)
}

if noWait { os.Exit(0) }

if waitHealthy(ctx, stack, timeout, changedServices(plan)) {
    os.Exit(0)
} else {
    if err := rollback(ctx, snapshot, rbTimeout); err != nil {
        os.Exit(3)
    }
    os.Exit(2)
}
```

## 8. Подробности health‑логики
- **Source of truth**: `Tasks` + `ContainerInspect` health.
- **No healthcheck**: статус `none` не блокирует релиз.
- **Unhealthy** любой активной задачи блокирует сервис.
- **Рестарты**: watcher отслеживает замену контейнера задачей, привязка по `TaskID`.
- **Промахи событий**: периодический reconcile — перечитать список задач сервиса и сверить с картой watcher’ов.

## 9. Откат (Rollback)
- **Снапшот перед apply**:
  - Для каждого сервиса: `ServiceInspectWithRaw` → сохраняем `Spec` и метаданные.
  - Для configs/secrets: метаданные. Если содержимое меняет утилита — сохранить payload в `snapshot/blobs/`.
- **Действия отката**:
  - Сервисы, существовавшие ранее: `ServiceUpdate` прежним `Spec`.
  - Новые сервисы: `ServiceRemove`.
  - Ресурсы:
    - если “первый релиз” и ресурс создан утилитой — удалить;
    - `external: true` не трогаем.
- **Ожидание завершения отката**: та же health‑логика, таймаут `--rollback-timeout` (10m).
- **Коды выхода**:
  - 0 — релиз успешен; либо релиз сорвался, но откат успешен.
  - 2 — релиз таймаут, откат успешен.
  - 3 — откат таймаут/ошибка.

## 10. Обработка сигналов
- Подписка на SIGINT, SIGTERM.
- При получении:
  - останавливаем новое ожидание;
  - запускаем откат;
  - завершение по правилам раздела 9.

## 11. CLI
Рабочее имя: `swarm-helm`

Подкоманды:
- `apply -f docker-compose.yml -n <stack> [--values values.yaml] [--set k=v] [--prune] [--timeout 15m] [--rollback-timeout 10m] [--parallel 4] [--no-wait]`
- `rollback -n <stack> [--snapshot PATH] [--rollback-timeout 10m]`
- `diff -f docker-compose.yml -n <stack> [--values values.yaml] [--set k=v]`
- `status -n <stack>`
- `logs -n <stack> [--follow] [--since 10m] [SERVICE]`
- `events -n <stack> [SERVICE]`

Глобальные флаги:
- `--docker-host`, `--tls`, `--cert-path` или ENV `DOCKER_HOST`, `DOCKER_TLS_VERIFY`, `DOCKER_CERT_PATH`.
- `--debug`, `--json`.

## 12. Выходные коды
- 0 — успех.
- 1 — ошибка валидации/парсинга.
- 2 — релиз таймаут, откат успешен.
- 3 — релиз сорвался и откат неудачен.
- 4 — ошибки соединения/авторизации Docker API/Registry.

## 13. Логирование
- Читабельный вывод по умолчанию.
- `--json` — структурированные события: время, модуль, уровень, объект, действие, состояние.
- Каналы: `planner`, `applier`, `watcher`, `rollback`, `signals`.

## 14. Архитектура пакетов
- `cmd/` — CLI (на cobra/urfave — допускается любая из них, но без новых зависимостей предпочтительно стандартный `flag`).
- `internal/compose/` — парсер YAML → внутренняя модель (минимальный Compose‑подмножество).
- `internal/plan/` — diff и план применения.
- `internal/swarm/` — обёртка над Docker client: сервисы, задачи, события, контейнеры, ресурсы.
- `internal/apply/` — применение плана, контроль версий `Service.Version.Index`, ретраи.
- `internal/health/` — подписки, watcher’ы, агрегатор состояния.
- `internal/snapshot/` — формат снапшота и хранение на диске.
- `internal/rollback/` — операция отката + ожидание.
- `internal/signals/` — SIGINT/SIGTERM, контексты.
- `internal/output/` — форматирование и прогресс.
- `internal/paths/` — резолвинг `SWARM_STACK_PATH` и конвертация путей.

## 15. Валидация и защитные меры
- Запрет `image: latest` без `--allow-latest`.
- Проверка конфликтов имён сетей/секретов/конфигов.
- Контроль гонок версии при `ServiceUpdate` (чтение/ретрай с backoff).
- Периодический reconcile задач для защиты от потерь событий.
- Не логировать содержимое `secrets`.

## 16. Конфигурация и ENV
- `DOCKER_HOST`, `DOCKER_TLS_VERIFY`, `DOCKER_CERT_PATH` — для подключения к Docker.
- `DOCKER_CONFIG_PATH` — путь к каталогу с `config.json` (auth).
- `SWARM_STACK_PATH` — базовый путь для относительных монтирований.
- `SWARM_HELM_TIMEOUT`, `SWARM_HELM_ROLLBACK_TIMEOUT`.
- `LOG_LEVEL`, `NO_COLOR`.

## 17. Формат снапшота
- Директория снапшота:
  - `snapshot.json`:
    ```json
    {
      "stack": "<name>",
      "createdAt": "<RFC3339>",
      "services": [
        {
          "name": "web",
          "spec": { /* swarm.ServiceSpec JSON */ },
          "meta": { "labels": {...} }
        }
      ],
      "resources": {
        "networks": [ { "name": "net1", "driver": "overlay", "labels": {...} } ],
        "configs":  [ { "name": "cfg1", "labels": {...}, "blob": "blobs/cfg1" } ],
        "secrets":  [ { "name": "sec1", "labels": {...}, "blob": "blobs/sec1" } ],
        "volumes":  [ { "name": "vol1", "driver": "local", "labels": {...} } ]
      }
    }
    ```
  - `blobs/` — бинарное содержимое configs/secrets при необходимости.
- Сжатие опционально. Путь задаётся `--snapshot PATH`. По умолчанию — временный каталог.

## 18. Тест‑план

### Юнит‑тесты
- Парсер Compose:
  - services: image/command/env/labels/ports/networks/volumes/healthcheck.
  - deploy: replicas/update_config/restart_policy/placement.
  - secrets/configs маппинг.
  - конвертация путей на основе `SWARM_STACK_PATH` и `cwd`.
- Diff‑логика: корректный план Create/Update/Delete для сервисов и ресурсов.
- Планировщик применений: корректный порядок и обработка ошибок.

### Интеграционные тесты (локальный Swarm)
- Инициализация стека из 2 сервисов, overlay сети, config, secret.
- Обновление образа с `update_config` и последующее `UpdateStatus.State == completed`.
- Симуляция `unhealthy` → ожидание → откат.
- SIGINT во время ожидания → откат.
- Первый релиз с неуспехом → удаление созданных объектов.
- Проверка pull через `DOCKER_CONFIG_PATH` (приватные образы).

### Негативные
- Потеря соединения с Docker API.
- Конфликт имён ресурсов.
- `latest` без разрешения.
- Недоступный Registry или неверный `config.json`.

## 19. Производительность и надёжность
- Параллельное применение сервисов `--parallel`.
- Ретраи API с экспоненциальным backoff.
- Ограничение буфера логов watcher’ов (`--logs-buffer`).
- Идемпотентность: повторный `apply` без изменений даёт пустой план и быстрый success.

## 20. Безопасность
- Не логировать секреты и приватные токены.
- Минимизировать доступ: только Docker socket/защищённый TCP, корректная TLS‑конфигурация.
- Аудит‑логи действий (опционально в `--json`).

## 21. Вывод пользователю
- `diff`‑вид до применения.
- Прогресс по сервисам: `UpdateState`, `Replicas Ready/Desired`, `Health OK/Fail`.
- Итог: `Release Succeeded` или `Release Failed: timeout` + сводка причин. При откате — `Rollback Succeeded/Failed`.

## 22. Неясности и допущения
- В Swarm нет нативного объекта “stack”. Используем метку пространства имён.
- Для новых сервисов без `UpdateStatus` — валидируем стабилизацию задач.
- Health без явного `healthcheck` не блокирует успех.
- Pull приватных образов опирается на `DOCKER_CONFIG_PATH/config.json`.
