## Список изменений для соответствия ТЗ

### 1. **Архитектура и организация кода**

### 1.1. Переименование и реструктуризация

- ✅ **Зависимости**: Полностью соответствуют ТЗ (docker v28.5.1, go-units v0.5.0, golang.org/x/net v0.43.0, gopkg.in/yaml.v3 v3.0.1)
- ❌ **Имя утилиты**: Переименовать из `docker-stackwait` → `stackman`
- ❌ **Модуль**: Переименовать модуль `stackwait` → `stackman`

### 1.2. Реорганизация пакетов (по ТЗ раздел 14)

Текущая структура → Требуемая структура:

`
Создать новые пакеты:
- internal/plan/        # diff и планирование (нет)
- internal/apply/       # применение плана с ретраями (частично в deployer/)
- internal/health/      # watcher'ы и агрегатор (частично в task/ и monitor/)
- internal/snapshot/    # снапшоты (есть parts/snapshot.go, перенести)
- internal/rollback/    # откат с ожиданием (есть deployer/rollback.go, дополнить)
- internal/signals/     # обработка сигналов (частично в main.go)
- internal/output/      # форматирование вывода (нет)
- internal/paths/       # резолвинг путей (нет)

Переименовать:
- compose/        → internal/compose/
- deployer/       → internal/swarm/ (обёртка над Docker API)
- task/           → слить в internal/health/
- monitor/        → слить в internal/health/
`

---

---

### 2. **CLI интерфейс**

### 2.1. Текущий CLI

`docker-stackwait <stack-name> <compose-file> [timeout] [max-failed]`

### 2.2. Требуемый CLI (ТЗ раздел 11)

`
stackman apply -f docker-compose.yml -n <stack> [флаги]
stackman rollback -n <stack> [--snapshot PATH]
stackman diff -f docker-compose.yml -n <stack>
stackman status -n <stack>
stackman logs -n <stack> [--follow] [SERVICE]
stackman events -n <stack> [SERVICE]
`

**Изменения:**

- ❌ Добавить подкоманды: `apply`, `rollback`, `diff`, `status`, `logs`, `events`
- ❌ Изменить флаги на именованные: `f`, `n`, `-values`, `-set`, `-timeout`, `-rollback-timeout`, `-parallel`, `-no-wait`, `-prune`
- ❌ Добавить глобальные флаги: `-docker-host`, `-tls`, `-cert-path`, `-debug`, `-json`

---

---

### 3. **Парсинг и шаблонизация Compose**

### 3.1. Текущая реализация

- ✅ Парсинг через `gopkg.in/yaml.v3`
- ❌ **Нет шаблонизации**: `-values values.yaml` и `-set key=val` не реализованы
- ❌ **Нет env-подстановки**: `${VAR}` в compose-файле не обрабатывается

**Изменения (ТЗ раздел 4):**

- Добавить функцию `render(composeFile, values, set, env)` перед парсингом
- Реализовать подстановку `${VAR}` из переменных окружения
- Реализовать override из `-values values.yaml`
- Реализовать override из `-set key=val`

---

---

### 4. **Обработка путей (ТЗ раздел 2)**

### 4.1. Текущая реализация

- ❌ Нет конвертации относительных путей в абсолютные
- ❌ Нет поддержки `SWARM_STACK_PATH`

**Изменения:**

- Создать `internal/paths/` пакет
- Реализовать резолвинг `SWARM_STACK_PATH` или `cwd`
- Конвертировать `./path` и `../path` в абсолютные пути для volumes/bind mounts

Пример из ТЗ:

`
if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
    source = strings.Replace(source, "./", cwd+"/", 1)
    if strings.HasPrefix(parts[0], "../") {
        source = cwd + "/" + parts[0]
    }
} else if !strings.HasPrefix(source, "/") && !strings.Contains(source, "://") {
    source = cwd + "/" + source
}
`

---

---

### 5. **Планирование и diff (ТЗ раздел 5)**

### 5.1. Текущая реализация

- ✅ Есть определение текущего состояния
- ❌ **Нет явного построения diff-плана**
- ❌ **Нет вывода diff перед применением**

**Изменения:**

- Создать `internal/plan/` пакет
- Реализовать `diff(current, desired)` с планом Create/Update/Delete
- Вывод diff-плана для пользователя (ТЗ раздел 21)
- Поддержка порядка: networks → configs/secrets → services → garbage‑collect

---

---

### 6. **Снапшоты и откат**

### 6.1. Текущая реализация

- ✅ Есть базовый snapshot в `parts/snapshot.go`
- ✅ Есть базовый rollback в `deployer/rollback.go`
- ❌ **Нет сохранения снапшотов на диск** (ТЗ раздел 17)
- ❌ **Нет ожидания завершения отката с health-проверками**

**Изменения (ТЗ раздел 9, 17):**

- Сохранять снапшоты в `snapshot.json` + `blobs/`
- Добавить поддержку `-snapshot PATH`
- При откате применять ту же health-логику с `-rollback-timeout`
- Сохранять payload секретов/конфигов при необходимости

Формат снапшота:

`
{
  "stack": "<name>",
  "createdAt": "<RFC3339>",
  "services": [{"name": "web", "spec": {...}, "meta": {...}}],
  "resources": {
    "networks": [...],
    "configs": [...],
    "secrets": [...],
    "volumes": [...]
  }
}
`

---

---

### 7. **Health-проверки и мониторинг**

### 7.1. Текущая реализация

- ✅ Есть `task.Monitor`, `task.Watcher`
- ✅ Есть проверка `ContainerInspect` для health
- ✅ Есть отслеживание событий
- ❌ **Не проверяется `ServiceInspect().UpdateStatus.State == "completed"`** (ТЗ раздел 6)
- ❌ **Нет агрегатора состояния по сервису** (прогресс Ready/Desired)

**Изменения (ТЗ раздел 6, 8):**

- Добавить проверку `UpdateStatus.State == "completed"` для обновлённых сервисов
- Для новых сервисов проверять количество running/healthy реплик
- Создать агрегатор в `internal/health/`:
    - Прогресс реплик Ready/Desired
    - Карта `containerID -> health {healthy|unhealthy|none}`
    - Последнее `UpdateStatus` сервиса
- Периодический reconcile для защиты от потерь событий

**Критерий успешного релиза:**

`UpdateStatus.State == "completed"   AND all_containers(State.Health.Status == healthy OR no_healthcheck)`

---

---

### 8. **Алгоритм apply (ТЗ раздел 7)**

### 8.1. Текущий flow

`main() → Deploy() → waitForAllTasksHealthy()`

### 8.2. Требуемый flow

```
ctx := withSignalsAndTimeout(...)
desired := parseCompose(render(composeFile, values, set, env))
current := readSwarmState(stackLabel)
snapshot := takeSnapshot(current, stackLabel)

plan := diff(current, desired)
if plan.Empty() && !noWait {
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

**Изменения:**

- Реализовать этот алгоритм в `cmd/apply.go`
- Добавить обработку пустого плана
- Разделить применение ресурсов и сервисов
- Добавить флаг `-no-wait`

---

---

### 9. **Обработка сигналов (ТЗ раздел 10)**

### 9.1. Текущая реализация

`
go func() {
    sig := <-sigChan
    select {
    case <-deploymentComplete:
        os.Exit(0)
    default:
        parts.Rollback(...)
        os.Exit(130)
    }
}()
`

### 9.2. Требуемая реализация

- ✅ Обработка SIGINT/SIGTERM есть
- ❌ Откат должен выполняться **всегда** при прерывании
- ❌ Нужен пакет `internal/signals/` для управления контекстами

**Изменения:**

- Вынести логику в `internal/signals/`
- Использовать контексты с отменой
- При сигнале: stop waiting → rollback → exit по правилам раздела 9

---

---

### 10. **Коды выхода (ТЗ раздел 12)**

### 10.1. Текущие коды

- 0 — успех
- 1 — ошибка
- 130 — SIGINT

### 10.2. Требуемые коды

- 0 — успех (релиз успешен ИЛИ релиз сорвался но откат успешен)
- 1 — ошибка валидации/парсинга
- 2 — релиз таймаут, откат успешен
- 3 — релиз сорвался и откат неудачен
- 4 — ошибки соединения/авторизации Docker API/Registry

**Изменения:**

- Изменить логику exit codes
- При сбое релиза но успешном откате → exit 0 (не 1!)
- При таймауте → exit 2
- При сбое отката → exit 3
- При проблемах с Docker/Registry → exit 4

---

---

### 11. **Логирование (ТЗ раздел 13)**

### 11.1. Текущее логирование

- Используется стандартный `log.Printf()`
- Нет структурированных логов

### 11.2. Требуемое (ТЗ раздел 13)

- Читабельный вывод по умолчанию
- `-json` — структурированные события: время, модуль, уровень, объект, действие, состояние
- Каналы: `planner`, `applier`, `watcher`, `rollback`, `signals`

**Изменения:**

- Создать `internal/output/` пакет
- Реализовать форматтеры для обычного и JSON вывода
- Добавить уровни: debug, info, warn, error
- Добавить каналы/модули к каждому сообщению

---

---

### 12. **Валидация и защитные меры (ТЗ раздел 15)**

### 12.1. Текущая реализация

- ❌ Нет защиты от `image: latest`
- ❌ Нет проверки конфликтов имён
- ❌ Нет контроля гонок версии при `ServiceUpdate`
- ❌ Нет периодического reconcile задач

**Изменения:**

- Запретить `image: latest` без `-allow-latest`
- Проверять конфликты имён сетей/секретов/конфигов
- Добавить retry с backoff при конфликтах версий `ServiceUpdate`
- Реализовать reconcile: периодически перечитывать список задач и сверять с watcher'ами
- Не логировать содержимое секретов

---

---

### 13. **Поддержка секретов и конфигов (ТЗ раздел 3)**

### 13.1. Текущая реализация

- ✅ Парсятся `secrets` и `configs` в compose
- ❌ **Не создаются** в Swarm
- ❌ Нет маппинга в `ContainerSpec.Secrets/Configs`

**Изменения:**

- Реализовать создание/обновление секретов через Docker API
- Реализовать создание/обновление конфигов через Docker API
- Добавить маппинг в `ContainerSpec` с `File` target
- При откате восстанавливать payload из `snapshot/blobs/`

---

---

### 14. **Поддерживаемые сущности Compose (ТЗ раздел 3)**

Проверить маппинг всех полей:

### 14.1. Services

- ✅ `image`, `command`, `environment`
- ❌ `args` (отдельно от command)
- ❌ `env_file` (слияние с environment)
- ✅ `labels`, `deploy.replicas`, `deploy.update_config`, `deploy.restart_policy`
- ❌ `deploy.placement.constraints` → не полностью
- ✅ `ports` → `EndpointSpec.Ports`
- ✅ `networks`
- ✅ `volumes` (но нет конвертации путей)
- ✅ `healthcheck`
- ❌ `secrets`, `configs` (не создаются)
- ❌ `resources.limits/reservations` (проверить)

### 14.2. Resources

- ✅ `networks` (overlay)
- ✅ `volumes` (local)
- ❌ `secrets` (не создаются)
- ❌ `configs` (не создаются)

---

---

### 15. **Дополнительные подкоманды (ТЗ раздел 11)**

### 15.1. Не реализованы

- ❌ `rollback` — откат по снапшоту
- ❌ `diff` — показать diff без применения
- ❌ `status` — текущее состояние стека
- ❌ `logs` — логи сервисов
- ❌ `events` — события стека

**Изменения:**

- Создать `cmd/rollback.go`
- Создать `cmd/diff.go`
- Создать `cmd/status.go`
- Создать `cmd/logs.go` (есть частично в `monitor/logs.go`, переиспользовать)
- Создать `cmd/events.go` (есть частично в `monitor/events.go`, переиспользовать)

---

---

### 16. **Переменные окружения (ТЗ раздел 16)**

### 16.1. Текущие

- ✅ `DOCKER_HOST`, `DOCKER_TLS_VERIFY`, `DOCKER_CERT_PATH` (через `client.FromEnv`)
- ✅ `DOCKER_CONFIG_PATH`
- ✅ `HEALTH_TIMEOUT_MINUTES`
- ✅ `MAX_FAILED_TASKS`

### 16.2. Требуемые дополнительно

- ❌ `SWARM_STACK_PATH` — базовый путь для относительных монтирований
- ❌ `SWARM_HELM_TIMEOUT`
- ❌ `SWARM_HELM_ROLLBACK_TIMEOUT`
- ❌ `LOG_LEVEL`
- ❌ `NO_COLOR`

---

---

### 17. **Производительность (ТЗ раздел 19)**

### 17.1. Текущая реализация

- ❌ Нет параллельного применения сервисов
- ✅ Есть ретраи с backoff (частично)
- ❌ Нет ограничения буфера логов watcher'ов

**Изменения:**

- Добавить флаг `-parallel` для параллельного применения сервисов
- Реализовать ретраи с экспоненциальным backoff для всех Docker API вызовов
- Добавить `-logs-buffer` для ограничения буфера
- Проверить идемпотентность: повторный apply без изменений → пустой план → быстрый success

---

---

### 18. **Вывод пользователю (ТЗ раздел 21)**

### 18.1. Текущий вывод

- Базовые логи через `log.Printf()`

### 18.2. Требуемый вывод

- diff-вид до применения
- Прогресс по сервисам: `UpdateState`, `Replicas Ready/Desired`, `Health OK/Fail`
- Итог: `Release Succeeded` или `Release Failed: timeout` + сводка причин
- При откате: `Rollback Succeeded/Failed`

**Изменения:**

- Добавить pretty-print для diff
- Добавить прогресс-бары или таблицы состояния сервисов
- Финальное сообщение с итогом

---

---

### 19. **Тесты (ТЗ раздел 18)**

### 19.1. Текущее покрытие

- ✅ Есть unit-тесты для compose parser
- ✅ Есть e2e тесты
- ✅ Есть тесты для task events

### 19.2. Требуемое покрытие

- ❌ Тесты для diff-логики
- ❌ Тесты для планировщика применений
- ❌ Тесты конвертации путей
- ❌ Интеграционные тесты симуляции unhealthy → откат
- ❌ Тесты SIGINT во время ожидания → откат
- ❌ Тесты первого релиза с неуспехом → удаление созданных объектов
- ❌ Негативные тесты

---

---

## Итоговая сводка изменений

### Критичные изменения (Must Have)

1. ✅ **Зависимости** — соответствуют
2. ❌ **CLI интерфейс** — полностью переделать на подкоманды
3. ❌ **Переименование** — `docker-stackwait` → `stackman`
4. ❌ **Архитектура пакетов** — реорганизация по ТЗ раздел 14
5. ❌ **Планирование и diff** — создать `internal/plan/`
6. ❌ **Снапшоты на диск** — реализовать формат из ТЗ раздел 17
7. ❌ **Шаблонизация** — `-values`, `-set`, `${VAR}`
8. ❌ **Конвертация путей** — `internal/paths/`
9. ❌ **UpdateStatus.State** — проверка "completed"
10. ❌ **Коды выхода** — изменить логику (0 при успешном откате!)
11. ❌ **Secrets/Configs** — создание в Swarm
12. ❌ **Валидация** — `latest`, конфликты, reconcile

### Важные изменения (Should Have)

1. ❌ **Логирование** — структурированные логи, `-json`
2. ❌ **Прогресс** — вывод diff, состояние сервисов
3. ❌ **Подкоманды** — `rollback`, `diff`, `status`, `logs`, `events`
4. ❌ **Переменные окружения** — `SWARM_STACK_PATH`, `LOG_LEVEL`, etc.
5. ❌ **Параллелизм** — `-parallel`

### Желательные изменения (Nice to Have)

1. ❌ **Тесты** — расширенное покрытие
2. ❌ **Производительность** — буферы, оптимизации