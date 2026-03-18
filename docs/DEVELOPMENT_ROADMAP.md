# Stackman — Карта разработки с нуля

## Обзор приложения

**Stackman** — инструмент командной строки (CLI) для оркестрации Docker Swarm стеков с поддержкой:
- Zero-downtime деплойментов с мониторингом здоровья
- Автоматического отката при ошибках
- Потоковой передачи логов контейнеров
- Event-driven отслеживания жизненного цикла задач

---

## Технологический стек

| Компонент         | Технология              |
|-------------------|-------------------------|
| Язык              | Go 1.24+                |
| Контейнеры        | Docker Swarm API        |
| Парсинг YAML      | gopkg.in/yaml.v3        |
| Docker Client     | github.com/docker/docker |
| Сборка            | Makefile + GitHub Actions |
| Контейнеризация   | Multi-arch Dockerfile   |
| Тестирование      | Go testing + codecov    |

---

## Этапы разработки

### Этап 0 — Подготовка проекта

**Цель:** Создать скелет проекта, настроить инфраструктуру разработки.

#### Задачи:
- [ ] Инициализировать Go-модуль: `go mod init github.com/SomeBlackMagic/stackman`
- [ ] Создать базовую структуру директорий:
  ```
  cmd/
  internal/
  tests/
  docs/
  ```
- [ ] Создать `main.go` — точку входа
- [ ] Настроить `Makefile` с целями: `build`, `test`, `lint`, `docker-build`
- [ ] Написать `Dockerfile` (multi-stage, multi-arch: amd64/arm64)
- [ ] Настроить `.github/workflows/build.yml` для CI/CD
- [ ] Добавить `.gitignore`, `LICENSE` (GPL-3.0), `README.md`
- [ ] Подключить зависимости: `go get github.com/docker/docker@v28+`

**Результат:** Проект компилируется и запускается командой `./stackman --help`.

---

### Этап 1 — CLI-интерфейс (`cmd/`)

**Цель:** Построить каркас CLI с маршрутизацией команд.

#### Архитектура:
```
cmd/
├── root.go      # Корневая команда, глобальные флаги
├── apply.go     # Команда deploy (заглушка)
├── rollback.go  # Команда rollback (заглушка)
├── logs.go      # Команда logs (заглушка)
├── events.go    # Команда events (заглушка)
├── version.go   # Команда version
└── stubs.go     # Нереализованные: diff, status
```

#### Задачи:
- [ ] `cmd/root.go` — инициализация CLI, парсинг глобальных флагов (`--timeout`, `--no-log-prefix`)
- [ ] `cmd/version.go` — вывод версии (переменные `Version`, `Commit`, `BuildDate` из ldflags)
- [ ] Заглушки для остальных команд с сообщением "Not implemented yet"
- [ ] Базовые флаги команды `apply`:
  - `-n / --name` — имя стека (обязательный)
  - `-f / --compose-file` — путь к compose-файлу (обязательный)
  - `--timeout` — таймаут деплоя (default: 5m)
  - `--prune` — удалять orphaned ресурсы
  - `--no-logs` — отключить стриминг логов

**Результат:** CLI запускается, все команды видны в `--help`, `version` работает.

---

### Этап 2 — Парсинг Docker Compose файлов (`internal/compose/`)

**Цель:** Читать и валидировать Docker Compose v3 файлы, конвертировать в Docker Swarm спецификации.

#### Архитектура:
```
internal/compose/
├── types.go        # Структуры данных Compose-файла
├── parser.go       # YAML-парсер
├── converter.go    # Compose → Docker Swarm spec
├── extra_hosts.go  # Резолвинг host-gateway
└── *_test.go
```

#### Задачи:

**`types.go`** — определить Go-структуры:
```go
type ComposeFile struct {
    Version  string              `yaml:"version"`
    Services map[string]Service  `yaml:"services"`
    Networks map[string]Network  `yaml:"networks"`
    Volumes  map[string]Volume   `yaml:"volumes"`
    Secrets  map[string]Secret   `yaml:"secrets"`
    Configs  map[string]Config   `yaml:"configs"`
}

type Service struct {
    Image       string
    Deploy      DeployConfig
    HealthCheck *HealthCheckConfig
    Environment []string
    Volumes     []VolumeMount
    Networks    []string
    Ports       []string
    ExtraHosts  []string
    // ... остальные поля
}

type DeployConfig struct {
    Replicas      *uint64
    UpdateConfig  *UpdateConfig
    RestartPolicy *RestartPolicy
    Resources     Resources
    Placement     Placement
}
```

**`parser.go`** — загрузка YAML:
```go
func ParseFile(path string) (*ComposeFile, error)
func ParseBytes(data []byte) (*ComposeFile, error)
```

**`converter.go`** — конвертация в Swarm spec:
```go
func ConvertService(name string, svc Service, stackName string) (swarm.ServiceSpec, error)
func ConvertNetwork(name string, net Network, stackName string) (types.NetworkCreate, error)
func ConvertVolume(name string, vol Volume, stackName string) (volume.CreateOptions, error)
```

**`extra_hosts.go`** — резолвинг `host-gateway`:
```go
func ResolveHostGateway() (string, error)
func ReplaceHostGatewayToken(hosts []string, gatewayIP string) []string
```

- [ ] Реализовать все структуры типов
- [ ] Реализовать YAML-парсер с валидацией версии
- [ ] Конвертер: сервисы, сети, тома
- [ ] Резолвинг extra_hosts с `host-gateway` токеном
- [ ] Написать unit-тесты для каждого компонента

**Результат:** `stackman apply` может прочитать compose-файл и сконвертировать его.

---

### Этап 3 — Генерация Deployment ID (`internal/deployment/`)

**Цель:** Уникально идентифицировать каждый деплоймент для трекинга задач.

#### Архитектура:
```
internal/deployment/
├── id.go       # Генерация ID
└── id_test.go
```

#### Задачи:
- [ ] Формат ID: `deploy-YYYYMMDD-HHMMSS-RANDOM8CHARS`
  - Пример: `deploy-20250107-143052-a1b2c3d4`
- [ ] Функция `GenerateID() string`
- [ ] Label для контейнеров: `com.stackman.deploy.id`
- [ ] Написать unit-тест с проверкой формата

**Результат:** Каждый деплоймент получает уникальный трекинг-идентификатор.

---

### Этап 4 — Утилиты путей (`internal/paths/`)

**Цель:** Вспомогательные утилиты для резолвинга путей.

#### Задачи:
- [ ] `resolver.go` — резолвинг относительных путей к compose-файлам
- [ ] Поддержка `~` в путях
- [ ] Абсолютизация путей относительно рабочей директории

---

### Этап 5 — Взаимодействие с Docker Swarm (`internal/swarm/`)

**Цель:** Реализовать полный жизненный цикл деплоймента через Docker API.

#### Архитектура:
```
internal/swarm/
├── interface.go          # DockerClient интерфейс (абстракция над Docker API)
├── stack.go              # StackDeployer — главный оркестратор
├── services.go           # Деплой сервисов
├── networks.go           # Создание сетей
├── volumes.go            # Создание томов
├── state.go              # Управление текущим состоянием стека
├── gateway.go            # Определение IP host-gateway в Swarm
├── cleanup.go            # Очистка orphaned ресурсов
├── rollback.go           # Откат деплоймента
├── mock_docker_client.go # Моки для тестов
└── *_test.go
```

#### Задачи:

**`interface.go`** — интерфейс Docker-клиента:
```go
type DockerClient interface {
    ServiceList(ctx, options) ([]swarm.Service, error)
    ServiceCreate(ctx, spec, options) (types.ServiceCreateResponse, error)
    ServiceUpdate(ctx, id string, version swarm.Version, spec swarm.ServiceSpec, options) (types.ServiceUpdateResponse, error)
    ServiceRemove(ctx, id string) error
    ServiceInspect(ctx, id string, options) (swarm.Service, types.RawMessage, error)
    TaskList(ctx, options) ([]swarm.Task, error)
    ContainerInspect(ctx, id string) (types.ContainerJSON, error)
    ContainerLogs(ctx, id string, options) (io.ReadCloser, error)
    NetworkCreate(ctx, name string, options) (types.NetworkCreateResponse, error)
    NetworkList(ctx, options) ([]types.NetworkResource, error)
    NetworkRemove(ctx, id string) error
    VolumeCreate(ctx, options) (volume.Volume, error)
    VolumeList(ctx, options) (volume.ListResponse, error)
    VolumeRemove(ctx, id string, force bool) error
    Events(ctx, options) (<-chan events.Message, <-chan error)
    NodeList(ctx, options) ([]swarm.Node, error)
    ImagePull(ctx, ref string, options) (io.ReadCloser, error)
}
```

**`stack.go`** — главный оркестратор `StackDeployer`:
```go
type StackDeployer struct {
    client     DockerClient
    stackName  string
    deployID   string
    composeFile *compose.ComposeFile
}

func (d *StackDeployer) Deploy(ctx context.Context, opts DeployOptions) error {
    // 1. Подготовка (cleanup exited containers)
    // 2. Удаление obsolete сервисов
    // 3. Pull образов
    // 4. Создание/обновление сетей
    // 5. Создание/обновление томов
    // 6. Деплой сервисов
    // 7. Отслеживание результатов
}
```

**`services.go`** — деплой сервисов:
```go
func (d *StackDeployer) deployServices(ctx, specs) ([]ServiceUpdateResult, error)
func (d *StackDeployer) createService(ctx, spec) (ServiceUpdateResult, error)
func (d *StackDeployer) updateService(ctx, existing, spec) (ServiceUpdateResult, error)
```

**`state.go`** — чтение текущего состояния:
```go
func (d *StackDeployer) getCurrentServices(ctx) (map[string]swarm.Service, error)
func (d *StackDeployer) getCurrentNetworks(ctx) (map[string]types.NetworkResource, error)
func (d *StackDeployer) getCurrentVolumes(ctx) (map[string]volume.Volume, error)
```

**`cleanup.go`** — очистка:
```go
func (d *StackDeployer) removeExitedContainers(ctx) error
func (d *StackDeployer) pruneOrphanedServices(ctx, desired map[string]bool) error
func (d *StackDeployer) pruneOrphanedNetworks(ctx, desired map[string]bool) error
```

**`rollback.go`** — откат:
```go
func (d *StackDeployer) Rollback(ctx context.Context) error
```

- [ ] Реализовать `DockerClient` интерфейс-обёртку над официальным Docker client
- [ ] Реализовать `StackDeployer` с полным workflow деплоя
- [ ] Реализовать деплой сервисов (create + update)
- [ ] Реализовать создание сетей и томов
- [ ] Реализовать `state.go` для чтения текущего состояния из Swarm
- [ ] Реализовать `cleanup.go` для очистки ресурсов
- [ ] Реализовать `gateway.go` для определения host-gateway IP
- [ ] Реализовать `rollback.go` для отката
- [ ] Создать mock-клиент для unit-тестов
- [ ] Написать unit-тесты

**Результат:** `stackman apply` может развернуть стек в Docker Swarm.

---

### Этап 6 — Снимки состояния (`internal/snapshot/`)

**Цель:** Сохранять состояние стека до деплоя для возможности отката.

#### Задачи:

**`snapshot.go`**:
```go
type StackSnapshot struct {
    Services []swarm.Service
    Networks []types.NetworkResource
    Volumes  []volume.Volume
    TakenAt  time.Time
}

func TakeSnapshot(ctx context.Context, client DockerClient, stackName string) (*StackSnapshot, error)
func (s *StackSnapshot) Rollback(ctx context.Context, client DockerClient) error
```

- [ ] Реализовать захват снимка перед деплоем
- [ ] Реализовать откат по снимку
- [ ] Интегрировать в `StackDeployer`

**Результат:** Автоматический откат при ошибках деплоя.

---

### Этап 7 — Мониторинг здоровья (`internal/health/`)

**Цель:** Отслеживать жизненный цикл задач и проверять работоспособность сервисов в реальном времени.

#### Архитектура:
```
internal/health/
├── events.go                  # Определения типов событий
├── watcher.go                 # Стриминг событий Docker
├── monitor.go                 # Трекинг жизненного цикла задач
├── service_update_monitor.go  # Мониторинг обновления сервисов
└── *_test.go
```

#### Задачи:

**`events.go`** — типы событий:
```go
type TaskEventType string

const (
    TaskCreated   TaskEventType = "task_created"
    TaskStarted   TaskEventType = "task_started"
    TaskRunning   TaskEventType = "task_running"
    TaskHealthy   TaskEventType = "task_healthy"
    TaskFailed    TaskEventType = "task_failed"
    TaskUnhealthy TaskEventType = "task_unhealthy"
    TaskShutdown  TaskEventType = "task_shutdown"
)

type TaskEvent struct {
    Type      TaskEventType
    TaskID    string
    ServiceID string
    NodeID    string
    Timestamp time.Time
}
```

**`watcher.go`** — Docker event stream:
```go
type EventWatcher struct {
    client DockerClient
}

func (w *EventWatcher) Watch(ctx context.Context) (<-chan TaskEvent, <-chan error)
// Подписывается на Docker events API, конвертирует raw events в TaskEvent
```

**`monitor.go`** — отслеживание жизненного цикла задачи:
```go
type TaskMonitor struct {
    client    DockerClient
    taskID    string
    serviceID string
    deployID  string
}

func (m *TaskMonitor) Monitor(ctx context.Context) (<-chan TaskEvent, error)
// Периодически инспектирует задачу, отслеживает состояние контейнера
// Стримит логи при необходимости
```

**`service_update_monitor.go`** — агрегация состояния сервиса:
```go
type ServiceUpdateMonitor struct {
    client    DockerClient
    serviceID string
    deployID  string
    replicas  int
}

func (m *ServiceUpdateMonitor) WaitHealthy(ctx context.Context, timeout time.Duration) error
// Ждёт пока все реплики сервиса не станут healthy
// Инспектирует задачи батчами для эффективности
```

- [ ] Реализовать типы событий
- [ ] Реализовать `EventWatcher` для стриминга Docker events
- [ ] Реализовать `TaskMonitor` с health-check polling
- [ ] Реализовать `ServiceUpdateMonitor` для агрегации по сервису
- [ ] Оптимизировать: батчинг API-запросов, параллельная инспекция контейнеров
- [ ] Написать unit-тесты

**Результат:** Система может ждать пока все реплики сервиса станут healthy.

---

### Этап 8 — Планировщик деплоя (`internal/plan/`)

**Цель:** Вычислять diff между текущим и желаемым состоянием без применения изменений.

#### Архитектура:
```
internal/plan/
├── types.go      # Структуры плана
├── planner.go    # Генерация плана
├── formatter.go  # Форматирование для вывода
└── *_test.go
```

#### Задачи:

**`types.go`**:
```go
type ChangeType string
const (
    ChangeCreate ChangeType = "create"
    ChangeUpdate ChangeType = "update"
    ChangeDelete ChangeType = "delete"
    ChangeNoOp   ChangeType = "no-op"
)

type ServiceChange struct {
    Name       string
    ChangeType ChangeType
    OldSpec    *swarm.ServiceSpec
    NewSpec    *swarm.ServiceSpec
}

type DeploymentPlan struct {
    Services []ServiceChange
    Networks []ResourceChange
    Volumes  []ResourceChange
}
```

**`planner.go`**:
```go
func CreatePlan(current StackState, desired *compose.ComposeFile) (*DeploymentPlan, error)
```

**`formatter.go`**:
```go
func FormatPlan(plan *DeploymentPlan) string
// Красивый вывод в стиле terraform plan
```

- [ ] Реализовать типы данных плана
- [ ] Реализовать `Planner` для сравнения состояний
- [ ] Реализовать форматтер для CLI-вывода
- [ ] Интегрировать в команду `diff`

---

### Этап 9 — Полная реализация команды `apply` (`cmd/apply.go`)

**Цель:** Объединить все компоненты в единый workflow деплоя.

#### Задачи:

**Полный workflow:**
```
1. Парсинг флагов и аргументов
2. Парсинг compose-файла (internal/compose)
3. Создание Docker-клиента
4. Создание StackDeployer (internal/swarm)
5. Снимок текущего состояния (internal/snapshot)
6. Генерация deployment ID (internal/deployment)
7. Деплой стека:
   a. Cleanup exited containers
   b. Удаление obsolete сервисов (если --prune)
   c. Pull images
   d. Создание/обновление сетей
   e. Создание/обновление томов
   f. Деплой/обновление сервисов
8. Параллельный мониторинг здоровья (internal/health)
9. Стриминг логов (если не --no-logs)
10. Ожидание healthy state всех сервисов
11. Успех или откат при ошибке/таймауте
```

**Signal handling:**
```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
// При получении сигнала — инициировать откат
```

**Параллельный мониторинг:**
```go
var wg sync.WaitGroup
for _, svc := range services {
    wg.Add(1)
    go func(svc swarm.Service) {
        defer wg.Done()
        monitor := health.NewServiceUpdateMonitor(client, svc.ID, deployID, replicas)
        err := monitor.WaitHealthy(ctx, timeout)
        // ...
    }(svc)
}
```

- [ ] Реализовать полный apply workflow
- [ ] Добавить signal handling (SIGTERM/SIGINT → rollback)
- [ ] Добавить параллельный мониторинг сервисов
- [ ] Добавить стриминг логов с цветовыми индикаторами
- [ ] Добавить прогресс-индикаторы
- [ ] Обработка таймаутов с автоматическим откатом

---

### Этап 10 — Команда `rollback` (`cmd/rollback.go`)

**Цель:** Откатить стек к предыдущему состоянию.

#### Задачи:
- [ ] Флаги: `-n / --name` (имя стека), `--timeout`
- [ ] Использовать Docker Swarm native rollback API
- [ ] Вывод прогресса отката
- [ ] Обработка ошибок отката

---

### Этап 11 — Команда `logs` (`cmd/logs.go`)

**Цель:** Стримить логи контейнеров стека.

#### Задачи:
- [ ] Флаги: `-n / --name`, `--tail`, `--follow`, `--since`
- [ ] Получение всех сервисов стека
- [ ] Параллельный стриминг логов всех контейнеров
- [ ] Цветовые префиксы для различения сервисов
- [ ] Timestamping

---

### Этап 12 — Команда `events` (`cmd/events.go`)

**Цель:** Отображать Docker-события стека.

#### Задачи:
- [ ] Флаги: `-n / --name`, `--since`, `--until`
- [ ] Фильтрация событий по label стека
- [ ] Форматированный вывод событий

---

### Этап 13 — Интеграционные тесты (`tests/`)

**Цель:** Покрыть полные deployment scenarios с реальным Docker Swarm.

#### Архитектура:
```
tests/
├── helpers_test.go       # Вспомогательные функции
├── apply_test.go         # Тесты деплоя
├── health_test.go        # Тесты мониторинга здоровья
├── operations_test.go    # Тесты операций CRUD
├── resources_test.go     # Тесты создания ресурсов
├── negative_test.go      # Тесты failure-сценариев
└── testdata/
    ├── simple-stack.yml
    ├── healthcheck-demo.yml
    └── extra-hosts-stack.yml
```

#### Задачи:
- [ ] **apply_test.go** — базовый деплой, обновление сервиса
- [ ] **health_test.go** — healthy deployment, unhealthy detection, таймаут
- [ ] **operations_test.go** — create/update/delete operations
- [ ] **resources_test.go** — network creation, volume creation
- [ ] **negative_test.go** — failure scenarios, invalid compose files, rollback trigger
- [ ] Создать тестовые compose-файлы в `testdata/`
- [ ] Настроить Makefile targets для запуска integration tests
- [ ] Добавить codecov интеграцию

---

### Этап 14 — Финальная полировка и документация

#### Задачи:
- [ ] Обновить `README.md` с полной документацией:
  - Installation guide
  - Usage examples
  - Configuration reference
  - Troubleshooting
- [ ] Добавить примеры compose-файлов
- [ ] Настроить GitHub Actions для CI/CD:
  - Тесты с race detection
  - codecov upload
  - Multi-arch Docker build
  - Release binaries
- [ ] Настроить dependabot для dependency scanning
- [ ] Финальный code review

---

## Граф зависимостей компонентов

```
main.go
  └── cmd/root.go
        ├── cmd/apply.go
        │     ├── internal/compose/     (парсинг файла)
        │     ├── internal/deployment/  (генерация ID)
        │     ├── internal/paths/       (резолвинг путей)
        │     ├── internal/snapshot/    (снимок для отката)
        │     ├── internal/swarm/       (деплой в Swarm)
        │     │     └── internal/compose/ (конвертация spec)
        │     └── internal/health/      (мониторинг здоровья)
        │           └── internal/swarm/ (Docker client)
        ├── cmd/rollback.go
        │     └── internal/swarm/
        ├── cmd/logs.go
        │     └── internal/swarm/
        ├── cmd/events.go
        │     └── internal/swarm/
        └── cmd/version.go
```

---

## Рекомендуемый порядок разработки

| # | Этап | Приоритет | Зависимости |
|---|------|-----------|-------------|
| 0 | Подготовка проекта | Критический | — |
| 1 | CLI-интерфейс | Высокий | 0 |
| 2 | Парсинг Compose | Высокий | 0 |
| 3 | Deployment ID | Средний | 0 |
| 4 | Утилиты путей | Низкий | 0 |
| 5 | Docker Swarm интеграция | Критический | 2, 3 |
| 6 | Снимки состояния | Высокий | 5 |
| 7 | Мониторинг здоровья | Критический | 5 |
| 8 | Планировщик | Низкий | 2, 5 |
| 9 | Команда apply | Критический | 1, 2, 5, 6, 7 |
| 10 | Команда rollback | Высокий | 5, 6 |
| 11 | Команда logs | Средний | 5 |
| 12 | Команда events | Низкий | 5 |
| 13 | Интеграционные тесты | Высокий | 9, 10, 11 |
| 14 | Документация | Средний | все |

---

## MVP (Минимально жизнеспособный продукт)

Для первого рабочего релиза необходимы этапы: **0, 1, 2, 3, 5, 7, 9**

Это даёт:
- `stackman apply -n mystack -f docker-compose.yml`
- Деплой в Docker Swarm
- Ожидание healthy state
- Автоматический откат при ошибке

---

## Оценка объёма кода

| Компонент | Файлов | Строк кода (ориентировочно) |
|-----------|--------|------------------------------|
| cmd/ | 7 | ~800 |
| internal/compose/ | 5 | ~600 |
| internal/swarm/ | 9 | ~900 |
| internal/health/ | 5 | ~700 |
| internal/deployment/ | 2 | ~50 |
| internal/snapshot/ | 1 | ~150 |
| internal/plan/ | 4 | ~300 |
| internal/paths/ | 1 | ~50 |
| tests/ | 6 | ~600 |
| **Итого** | **~40** | **~4150** |

---

## Ключевые паттерны проектирования

1. **Interface-based abstraction** — `DockerClient` интерфейс для тестируемости
2. **Event-driven architecture** — паттерн Watcher/Monitor для real-time обновлений
3. **Goroutine orchestration** — `sync.WaitGroup` и каналы для параллельного мониторинга
4. **Context propagation** — таймауты и отмена через `context.Context`
5. **Snapshot pattern** — захват состояния для отката
6. **Label-based filtering** — пространство имён стека через `com.docker.stack.namespace`

---

*Документ создан: 2026-03-18*
*Версия: 1.0*
