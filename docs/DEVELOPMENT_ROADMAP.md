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

## Кросс-компонентные требования

> Применяются ко **всем** этапам. Реализуются один раз, соблюдаются везде.

### Коды выхода CLI

| Код | Значение |
|-----|----------|
| `0` | Успешное завершение |
| `1` | Общая ошибка (невалидные аргументы, ошибка деплоя) |
| `2` | Таймаут ожидания healthy-состояния |
| `3` | Прерван сигналом SIGINT/SIGTERM — откат **выполнен** успешно |
| `4` | Прерван сигналом SIGINT/SIGTERM — откат **не выполнен** |

### Форматы вывода

- **TTY**: human-readable, с цветами; прогресс идёт в `stdout`
- **Pipe / CI**: автодетект `isatty` → без цветов, без эмодзи
- **`--output json`**: машиночитаемый JSON в `stdout`; прогресс и логи — в `stderr`
- **`--no-color`**: принудительно отключить цвета (также через `STACKMAN_NO_COLOR=1`)

### Логирование

Глобальный флаг: `--log-level <debug|info|warn|error>` (default: `info`)
Переменная окружения: `STACKMAN_LOG_LEVEL` (переопределяется флагом)

| Уровень | Когда использовать |
|---------|--------------------|
| `debug` | Сырые вызовы Docker API, внутренние события |
| `info`  | Ключевые шаги деплоя, переходы статусов |
| `warn`  | Нефатальные отклонения (повтор pull, retry) |
| `error` | Ошибки, после которых инициируется откат или выход |

Формат в TTY: `[INFO] сообщение`
Формат в pipe/CI: `{"level":"info","msg":"сообщение","time":"2006-01-02T15:04:05Z"}`

### Переменные окружения

| Переменная | Флаг | Описание |
|------------|------|----------|
| `DOCKER_HOST` | — | Адрес Docker daemon |
| `DOCKER_TLS_VERIFY` | — | Включить TLS-верификацию |
| `DOCKER_CERT_PATH` | — | Путь к TLS-сертификатам |
| `STACKMAN_TIMEOUT` | `--timeout` | Глобальный таймаут деплоя |
| `STACKMAN_LOG_LEVEL` | `--log-level` | Уровень логирования |
| `STACKMAN_NO_COLOR` | `--no-color` | Отключить цветной вывод |

### Определение "healthy" сервиса

Сервис считается **healthy** когда выполнены **оба** условия:
1. Все ожидаемые реплики (`Spec.Mode.Replicated.Replicas`) находятся в состоянии `Running`
2. Если у сервиса задан `healthcheck` — у всех реплик `ContainerStatus.Health.Status == "healthy"`

Реплика считается **failed** если:
- Состояние задачи: `Failed`, `Rejected` или `Orphaned`
- Либо `DesiredState = Shutdown` без явного масштабирования вниз

### Стратегия отмены при параллельном мониторинге

При обнаружении failed-реплики в **любом** сервисе:
1. Отменить `context` для всех goroutine мониторинга
2. Записать в лог: сервис, причину, состояние задачи
3. Инициировать откат (см. приоритеты ниже)
4. Завершить с кодом выхода `1`

### Приоритет механизмов отката

| Сценарий | Механизм |
|----------|----------|
| Ошибка во время `apply` | Snapshot rollback (`internal/snapshot`) |
| Ручная команда `rollback` | Docker Swarm `UpdateRollback` API → при ошибке fallback на snapshot |
| Snapshot недоступен | Docker Swarm `UpdateRollback` API |
| Первый деплой стека (snapshot пустой) | Удалить созданные сервисы/сети/тома |

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

**Критерии приёмки:**
- [ ] `go build ./...` завершается без ошибок и предупреждений
- [ ] `./stackman --help` выводит список команд без паники
- [ ] `make build test lint` — все три цели выполняются успешно
- [ ] `docker buildx build --platform linux/amd64,linux/arm64` — образ собирается
- [ ] GitHub Actions pipeline проходит на пустом коммите
- [ ] `go mod tidy` не изменяет `go.sum`

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

**Критерии приёмки:**
- [ ] `./stackman --help` показывает команды: `apply`, `rollback`, `logs`, `events`, `diff`, `status`, `version`
- [ ] `./stackman version` выводит три поля: `Version`, `Commit`, `BuildDate` (из ldflags)
- [ ] `./stackman apply --help` перечисляет все флаги: `-n`, `-f`, `--timeout`, `--prune`, `--no-logs`, `--output`, `--log-level`
- [ ] Вызов незаконченной команды (`diff`, `status`) возвращает код выхода `1` и сообщение "not implemented yet"
- [ ] `--log-level invalid` возвращает понятную ошибку и код `1`

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

**Критерии приёмки:**
- [ ] `ParseFile("testdata/valid.yml")` возвращает структуру без ошибки
- [ ] `ParseFile("testdata/invalid.yml")` возвращает описательную ошибку
- [ ] `ConvertService` для сервиса с `deploy.replicas: 3` возвращает `ServiceSpec` с `Replicated.Replicas = 3`
- [ ] `ConvertService` для сервиса с `healthcheck.disable: true` возвращает spec без healthcheck
- [ ] `ResolveHostGateway()` возвращает непустую строку на Linux без ошибки
- [ ] `ReplaceHostGatewayToken(["host-gateway:alias"], "172.17.0.1")` возвращает `["172.17.0.1:alias"]`
- [ ] `go test ./internal/compose/...` проходит без флага `-short`

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

**Критерии приёмки:**
- [ ] `GenerateID()` соответствует regexp `^deploy-\d{8}-\d{6}-[0-9a-f]{8}$`
- [ ] Два последовательных вызова `GenerateID()` возвращают разные значения
- [ ] `DeployIDLabel == "com.stackman.deploy.id"` — константа проверяется в тесте
- [ ] `go test ./internal/deployment/...` проходит

**Результат:** Каждый деплоймент получает уникальный трекинг-идентификатор.

---

### Этап 4 — Утилиты путей (`internal/paths/`)

**Цель:** Вспомогательные утилиты для резолвинга путей.

#### Задачи:
- [ ] `resolver.go` — резолвинг относительных путей к compose-файлам
- [ ] Поддержка `~` в путях
- [ ] Абсолютизация путей относительно рабочей директории

**Критерии приёмки:**
- [ ] `Resolve("./stack.yml")` возвращает абсолютный путь относительно CWD
- [ ] `Resolve("~/stacks/app.yml")` корректно разворачивает `~`
- [ ] `Resolve("/abs/path.yml")` возвращает путь без изменений
- [ ] `Resolve("nfs://host/vol")` возвращает строку без изменений
- [ ] `go test ./internal/paths/...` проходит

---

### Этап 5 — Взаимодействие с Docker Swarm (`internal/swarm/`)

> **Этап разбит на подэтапы** — каждый реализуется и тестируется независимо.
> Общая архитектура пакета:
> ```
> internal/swarm/
> ├── interface.go          # DockerClient интерфейс
> ├── mock_docker_client.go # Mock для тестов
> ├── state.go              # Чтение текущего состояния стека
> ├── gateway.go            # Определение IP host-gateway
> ├── networks.go           # Создание/проверка сетей
> ├── volumes.go            # Создание/проверка томов
> ├── images.go             # Pull образов
> ├── services.go           # Деплой сервисов (create + update)
> ├── cleanup.go            # Очистка orphaned ресурсов
> ├── rollback.go           # Откат деплоя
> ├── stack.go              # StackDeployer — главный оркестратор
> └── *_test.go
> ```

---

#### Этап 5а — Docker Client Abstraction (`interface.go`, `mock_docker_client.go`)

**Цель:** Определить интерфейс взаимодействия с Docker API и mock для тестов. Ни одна часть кода не должна импортировать Docker SDK напрямую — только через этот интерфейс.

**`interface.go`:**
```go
type DockerClient interface {
    ServiceList(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error)
    ServiceCreate(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (types.ServiceCreateResponse, error)
    ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (types.ServiceUpdateResponse, error)
    ServiceRemove(ctx context.Context, serviceID string) error
    ServiceInspectWithRaw(ctx context.Context, serviceID string, opts types.ServiceInspectOptions) (swarm.Service, []byte, error)
    TaskList(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error)
    ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
    ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
    ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
    ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)
    NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
    NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
    NetworkRemove(ctx context.Context, networkID string) error
    VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
    VolumeList(ctx context.Context, filter filters.Args) (volume.ListResponse, error)
    VolumeRemove(ctx context.Context, volumeID string, force bool) error
    Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error)
    NodeList(ctx context.Context, options types.NodeListOptions) ([]swarm.Node, error)
    ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
    Close() error
}
```

**`mock_docker_client.go`:** Реализация `DockerClient` с полями-функциями для подмены поведения в тестах:
```go
type MockDockerClient struct {
    ServiceListFn    func(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error)
    ServiceCreateFn  func(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (types.ServiceCreateResponse, error)
    // ... по одному полю на каждый метод интерфейса
}
```

**Задачи:**
- [ ] Определить интерфейс `DockerClient` со всеми методами
- [ ] Реализовать `MockDockerClient` — каждый метод делегирует соответствующему `Fn`-полю, если nil — паникует с понятным сообщением
- [ ] Убедиться что `var _ DockerClient = (*MockDockerClient)(nil)` компилируется

**Критерии приёмки:**
- [ ] `go build ./internal/swarm/` компилируется
- [ ] `var _ DockerClient = (*MockDockerClient)(nil)` проходит компиляцию
- [ ] Вызов метода на `MockDockerClient` без установленного `Fn` паникует с сообщением вида `MockDockerClient.ServiceList not set`
- [ ] `go test ./internal/swarm/ -run TestMock` проходит

---

#### Этап 5б — Состояние стека (`state.go`, `gateway.go`)

**Цель:** Получать текущий список ресурсов стека из Swarm через `com.docker.stack.namespace` label. Определять IP host-gateway для single-node режима.

**`state.go`:**
```go
// StackState — полный снимок текущего состояния стека в Swarm.
// Используется планировщиком (Этап 8) и snapshot (Этап 6).
type StackState struct {
    Services map[string]swarm.Service  // ключ — имя сервиса без префикса стека
    Networks map[string]network.Summary
    Volumes  map[string]volume.Volume
}

func GetStackState(ctx context.Context, client DockerClient, stackName string) (*StackState, error)
```

**`gateway.go`:**
```go
// IsMultiNodeSwarm — true если в кластере больше одной ноды.
func IsMultiNodeSwarm(ctx context.Context, client DockerClient) (bool, error)

// DetectHostGatewayIP — возвращает IP docker0-бриджа.
// Возвращает ошибку если кластер multi-node.
func DetectHostGatewayIP(ctx context.Context, client DockerClient) (string, error)
```

**Задачи:**
- [ ] Реализовать `GetStackState` с фильтрацией по `com.docker.stack.namespace=<stackName>`
- [ ] Реализовать `IsMultiNodeSwarm` через `NodeList`
- [ ] Реализовать `DetectHostGatewayIP`
- [ ] Unit-тесты с `MockDockerClient`

**Критерии приёмки:**
- [ ] `GetStackState` для несуществующего стека возвращает пустой `StackState` без ошибки
- [ ] `GetStackState` корректно фильтрует сервисы чужого стека
- [ ] `DetectHostGatewayIP` возвращает `ErrMultiNodeSwarm` для кластера из 2+ нод
- [ ] `go test ./internal/swarm/ -run TestState` проходит

---

#### Этап 5в — Сети и тома (`networks.go`, `volumes.go`)

**Цель:** Создавать сети и тома стека если они не существуют; не трогать уже существующие.

```go
// EnsureNetworks создаёт сети из compose-файла если их ещё нет.
// Идемпотентен: повторный вызов не создаёт дубликаты.
func EnsureNetworks(ctx context.Context, client DockerClient, stackName string, networks map[string]*compose.Network) error

// EnsureVolumes создаёт тома из compose-файла если их ещё нет.
func EnsureVolumes(ctx context.Context, client DockerClient, stackName string, volumes map[string]*compose.Volume) error
```

**Именование:** `<stackName>_<networkName>` / `<stackName>_<volumeName>` — соответствует поведению `docker stack deploy`.

**Задачи:**
- [ ] Реализовать `EnsureNetworks` — список существующих → создать отсутствующие
- [ ] Реализовать `EnsureVolumes`
- [ ] Тесты на идемпотентность (второй вызов не вызывает `NetworkCreate`)

**Критерии приёмки:**
- [ ] `EnsureNetworks` вызывает `NetworkCreate` только для отсутствующих сетей
- [ ] Повторный вызов с теми же аргументами не делает API-вызовов `NetworkCreate`
- [ ] `go test ./internal/swarm/ -run TestNetworks,TestVolumes` проходит

---

#### Этап 5г — Pull образов и деплой сервисов (`images.go`, `services.go`)

**Цель:** Сначала pull всех образов, затем создать или обновить сервисы с инъекцией deploy ID.

**`images.go`:**
```go
// PullImages скачивает образы всех сервисов параллельно.
// Сервисы с image: "" (build-only) пропускаются.
func PullImages(ctx context.Context, client DockerClient, services map[string]*compose.Service) error
```

**`services.go`:**
```go
type ServiceUpdateResult struct {
    ServiceID   string
    ServiceName string
    Version     swarm.Version
    Warnings    []string
    Created     bool   // true — создан новый, false — обновлён существующий
    DeployID    string
}

// DeployServices создаёт или обновляет все сервисы стека.
// Добавляет label com.stackman.deploy.id=<deployID> к каждому сервису и задаче.
func DeployServices(
    ctx context.Context,
    client DockerClient,
    stackName string,
    services map[string]*compose.Service,
    deployID string,
) ([]ServiceUpdateResult, error)
```

**Задачи:**
- [ ] Реализовать `PullImages` — параллельный pull, агрегация ошибок
- [ ] Реализовать `DeployServices` — для каждого сервиса: если существует → `ServiceUpdate`, если нет → `ServiceCreate`
- [ ] Инъекция `deployID` как label `com.stackman.deploy.id` в ServiceSpec и TaskTemplate
- [ ] Для `host-gateway`: резолвинг только на single-node (через `IsMultiNodeSwarm`)
- [ ] Тесты на create/update пути с MockDockerClient

**Критерии приёмки:**
- [ ] `DeployServices` для нового сервиса вызывает `ServiceCreate`, не `ServiceUpdate`
- [ ] `DeployServices` для существующего сервиса вызывает `ServiceUpdate` с корректным `Version`
- [ ] Результат содержит `DeployID` совпадающий с переданным
- [ ] `go test ./internal/swarm/ -run TestServices,TestImages` проходит

---

#### Этап 5д — Очистка ресурсов (`cleanup.go`)

**Цель:** Удалять завершённые контейнеры и orphaned ресурсы (сервисы/сети не из compose-файла).

```go
// RemoveExitedContainers удаляет контейнеры стека в статусе exited/dead.
func RemoveExitedContainers(ctx context.Context, client DockerClient, stackName string) error

// PruneOrphanedServices удаляет сервисы стека, отсутствующие в desired.
func PruneOrphanedServices(ctx context.Context, client DockerClient, stackName string, desired map[string]bool) error

// PruneOrphanedNetworks удаляет сети стека, отсутствующие в desired.
func PruneOrphanedNetworks(ctx context.Context, client DockerClient, stackName string, desired map[string]bool) error
```

**Задачи:**
- [ ] Реализовать все три функции
- [ ] `PruneOrphanedServices` вызывается только если флаг `--prune` установлен (логика флага — в `cmd/apply.go`)
- [ ] Тесты на: нет orphaned ресурсов → ничего не удаляется

**Критерии приёмки:**
- [ ] `PruneOrphanedServices` не трогает сервисы других стеков
- [ ] `RemoveExitedContainers` не трогает running контейнеры
- [ ] `go test ./internal/swarm/ -run TestCleanup` проходит

---

#### Этап 5е — Оркестратор деплоя (`stack.go`)

**Цель:** Собрать все подэтапы 5а–5д в единый `StackDeployer` с детерминированным порядком шагов.

```go
type DeployOptions struct {
    PruneOrphans bool
    NoPull       bool
}

type DeploymentResult struct {
    DeployID        string
    UpdatedServices []ServiceUpdateResult
}

type StackDeployer struct {
    client    DockerClient
    stackName string
}

func NewStackDeployer(client DockerClient, stackName string) *StackDeployer

// Deploy выполняет полный деплой стека в порядке:
// 1. RemoveExitedContainers
// 2. PruneOrphanedServices (если opts.PruneOrphans)
// 3. PullImages (если !opts.NoPull)
// 4. EnsureNetworks
// 5. EnsureVolumes
// 6. DeployServices
func (d *StackDeployer) Deploy(ctx context.Context, cf *compose.ComposeFile, deployID string, opts DeployOptions) (*DeploymentResult, error)

// GetState возвращает текущее состояние стека (делегирует GetStackState)
func (d *StackDeployer) GetState(ctx context.Context) (*StackState, error)
```

**Задачи:**
- [ ] Реализовать `StackDeployer.Deploy` с вызовом функций из 5а–5д в правильном порядке
- [ ] При ошибке на любом шаге — вернуть ошибку с контекстом шага (`fmt.Errorf("deploy networks: %w", err)`)
- [ ] Тест полного workflow с MockDockerClient

**Критерии приёмки:**
- [ ] При ошибке `PullImages` — `Deploy` возвращает ошибку, `DeployServices` не вызывается
- [ ] Ошибки оборачиваются через `%w` для `errors.Is`/`errors.As`
- [ ] `go test ./internal/swarm/ -run TestStackDeployer` проходит
- [ ] `go test ./internal/swarm/...` — все тесты зелёные

**Результат:** `stackman apply` может развернуть стек в Docker Swarm.

---

### Этап 6 — Снимки состояния (`internal/snapshot/`)

**Цель:** Сохранять состояние стека до деплоя для полного детерминированного отката.

#### Задачи:

**`snapshot.go`**:
```go
type StackSnapshot struct {
    StackName    string
    TakenAt      time.Time
    IsFirstDeploy bool  // true — стека не существовало, откат = полное удаление
    Services     map[string]swarm.Service  // ключ — имя сервиса
    Networks     map[string]network.Summary
    Volumes      map[string]volume.Volume
}

// TakeSnapshot захватывает текущее состояние стека через StackDeployer.GetState().
// Если стек не существует — возвращает snapshot с IsFirstDeploy=true.
func TakeSnapshot(ctx context.Context, deployer *swarm.StackDeployer) (*StackSnapshot, error)

// Rollback восстанавливает состояние стека по снимку:
// - Если IsFirstDeploy: удаляет все созданные сервисы, сети, тома
// - Иначе: обновляет каждый сервис до spec из снимка; удаляет сервисы появившиеся после снимка
func (s *StackSnapshot) Rollback(ctx context.Context, client swarm.DockerClient) error
```

**Задачи:**
- [ ] Реализовать `TakeSnapshot` с использованием `swarm.GetStackState`
- [ ] Реализовать `Rollback` — обработать оба случая (`IsFirstDeploy` и обновление)
- [ ] При откате залогировать каждый восстанавливаемый сервис на уровне `info`
- [ ] Unit-тест: snapshot → rollback → проверить что `ServiceUpdate` вызван с оригинальным spec

**Критерии приёмки:**
- [ ] `TakeSnapshot` для несуществующего стека устанавливает `IsFirstDeploy=true`
- [ ] `Rollback` при `IsFirstDeploy=true` вызывает `ServiceRemove` для каждого созданного сервиса
- [ ] `Rollback` при `IsFirstDeploy=false` вызывает `ServiceUpdate` с версией из снимка
- [ ] `Rollback` возвращает `error` — скомпилировать с `var _ error = s.Rollback(...)` не получится, функция возвращает `error`
- [ ] `go test ./internal/snapshot/...` проходит

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

**Критерии приёмки:**
- [ ] `EventWatcher.Watch` при получении события `container die` эмитит `TaskFailed`
- [ ] `TaskMonitor.Monitor` переходит в состояние `TaskHealthy` когда `ContainerStatus.Health.Status == "healthy"`
- [ ] `TaskMonitor.Monitor` переходит в состояние `TaskRunning` если healthcheck отсутствует и контейнер в Running
- [ ] `ServiceUpdateMonitor.WaitHealthy` возвращает `nil` когда все реплики в `TaskRunning`/`TaskHealthy`
- [ ] `ServiceUpdateMonitor.WaitHealthy` возвращает ошибку при таймауте
- [ ] `ServiceUpdateMonitor.WaitHealthy` возвращает ошибку при `TaskFailed` до таймаута
- [ ] `go test ./internal/health/...` проходит

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
// CreatePlan сравнивает текущее состояние стека с желаемым из compose-файла.
// current получается через swarm.GetStackState; desired — из compose.ParseFile.
func CreatePlan(current *swarm.StackState, desired *compose.ComposeFile, stackName string) (*DeploymentPlan, error)
```

**`formatter.go`**:
```go
func FormatPlan(plan *DeploymentPlan) string
// Красивый вывод в стиле terraform plan
```

- [ ] Реализовать типы данных плана
- [ ] Реализовать `Planner` для сравнения состояний
- [ ] Реализовать форматтер для CLI-вывода
- [ ] Интегрировать в команду `diff` (Этап 12а)

**Критерии приёмки:**
- [ ] `CreatePlan` для идентичных состояний возвращает план с `IsEmpty() == true`
- [ ] `CreatePlan` для нового сервиса возвращает `ServiceChange{ChangeType: ChangeCreate}`
- [ ] `CreatePlan` для удалённого из compose сервиса возвращает `ServiceChange{ChangeType: ChangeDelete}`
- [ ] `FormatPlan` выводит строки с префиксами `+`, `~`, `-` для create/update/delete
- [ ] `go test ./internal/plan/...` проходит

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

**Критерии приёмки:**
- [ ] Успешный деплой завершается с кодом `0`
- [ ] При таймауте — инициируется откат, код выхода `2`
- [ ] При SIGINT во время деплоя — инициируется откат, код `3` (успех) или `4` (неудача)
- [ ] `--no-logs` отключает стриминг логов, мониторинг здоровья продолжается
- [ ] `--prune` удаляет orphaned сервисы, без флага — не трогает
- [ ] Параллельный мониторинг: при падении одного сервиса отменяется мониторинг остальных

---

### Этап 10 — Команда `rollback` (`cmd/rollback.go`)

**Цель:** Откатить стек к предыдущему состоянию.

#### Задачи:
- [ ] Флаги: `-n / --name` (имя стека), `--timeout`
- [ ] Механизм: Docker Swarm `UpdateRollback` API → fallback на snapshot при ошибке (см. приоритеты в кросс-компонентных требованиях)
- [ ] Вывод прогресса отката
- [ ] Обработка ошибок отката

**Критерии приёмки:**
- [ ] Успешный откат завершается с кодом `0`
- [ ] Откат несуществующего стека (`-n unknown`) возвращает ошибку и код `1`
- [ ] `--timeout` прерывает ожидание и возвращает код `2`

---

### Этап 11 — Команда `logs` (`cmd/logs.go`)

**Цель:** Стримить логи контейнеров стека.

#### Задачи:
- [ ] Флаги: `-n / --name`, `--tail`, `--follow`, `--since`
- [ ] Получение всех сервисов стека
- [ ] Параллельный стриминг логов всех контейнеров
- [ ] Цветовые префиксы для различения сервисов
- [ ] Timestamping

**Критерии приёмки:**
- [ ] `--follow` держит соединение до Ctrl+C
- [ ] Без `--follow` выводит хвост логов и завершается с кодом `0`
- [ ] `--tail 50` выводит не более 50 строк на сервис
- [ ] Префикс каждой строки: `[service-name] <log line>`
- [ ] Без `--no-log-prefix` префикс есть; с флагом — отсутствует

---

### Этап 12 — Команда `events` (`cmd/events.go`)

**Цель:** Отображать Docker-события стека.

#### Задачи:
- [ ] Флаги: `-n / --name`, `--since`, `--until`
- [ ] Фильтрация событий по label стека
- [ ] Форматированный вывод событий

**Критерии приёмки:**
- [ ] Вывод содержит тип события, имя сервиса, timestamp
- [ ] `--since 5m` ограничивает историю
- [ ] `--until` завершает поток после заданного момента
- [ ] Ctrl+C завершает с кодом `0`

---

### Этап 12а — Команда `diff` (`cmd/diff.go`)

**Цель:** Показать план изменений между текущим состоянием стека и compose-файлом без применения изменений.

#### Задачи:
- [ ] Флаги: `-n / --name` (обязательный), `-f / --compose-file` (обязательный), `--output`
- [ ] Получить текущее состояние через `swarm.GetStackState`
- [ ] Распарсить compose-файл через `compose.ParseFile`
- [ ] Вычислить план через `plan.CreatePlan`
- [ ] Отформатировать через `plan.FormatPlan` и вывести в stdout
- [ ] Если изменений нет — вывести "No changes" и завершить с кодом `0`
- [ ] Если изменения есть — завершить с кодом `0` (diff — не ошибка)
- [ ] `--output json` — вывести план в JSON

**Критерии приёмки:**
- [ ] Для идентичного состояния выводит "No changes." и код `0`
- [ ] Для нового сервиса выводит строку с `+`
- [ ] Для удалённого сервиса выводит строку с `-`
- [ ] `--output json` выводит валидный JSON
- [ ] Без `--name` или `--compose-file` возвращает код `1` с понятной ошибкой

---

### Этап 12б — Команда `status` (`cmd/status.go`)

**Цель:** Показать текущее состояние сервисов стека: имя, количество реплик, статус, образ.

#### Задачи:
- [ ] Флаги: `-n / --name` (обязательный), `--output`
- [ ] Получить список сервисов через `swarm.GetStackState`
- [ ] Для каждого сервиса получить задачи через `TaskList` и посчитать running/total
- [ ] Вывод в формате таблицы:
  ```
  NAME              IMAGE                    REPLICAS   STATUS
  mystack_web       nginx:1.25               3/3        healthy
  mystack_worker    myapp:latest             2/3        updating
  ```
- [ ] `--output json` — вывести в JSON
- [ ] Если стек не найден — вывести ошибку и код `1`

**Критерии приёмки:**
- [ ] Для запущенного стека с 2 сервисами выводит 2 строки
- [ ] `REPLICAS` показывает `running/desired`
- [ ] `STATUS` показывает `healthy` / `updating` / `failed` / `partial`
- [ ] `--output json` выводит массив объектов с полями `name`, `image`, `replicas`, `status`
- [ ] Для несуществующего стека — код `1`, сообщение об ошибке

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
- [ ] Настроить Makefile targets для запуска integration tests (`make test-integration`)
- [ ] Добавить codecov интеграцию
- [ ] Добавить тестовые compose-файлы:
  - `testdata/simple-stack.yml` — один сервис без healthcheck
  - `testdata/healthcheck-demo.yml` — сервис с healthcheck
  - `testdata/extra-hosts-stack.yml` — host-gateway резолвинг
  - `testdata/multi-service.yml` — несколько взаимозависимых сервисов
  - `testdata/bad-image.yml` — несуществующий образ (для negative-тестов)

**Критерии приёмки:**
- [ ] `make test-integration` проходит на чистом Docker Swarm окружении
- [ ] `apply_test.go`: базовый деплой завершается healthy
- [ ] `apply_test.go`: повторный деплой с изменением образа обновляет сервис
- [ ] `health_test.go`: сервис с failing healthcheck приводит к откату
- [ ] `health_test.go`: таймаут при слишком долгом старте приводит к откату с кодом `2`
- [ ] `negative_test.go`: невалидный compose-файл → код `1`, без изменений в Swarm
- [ ] `operations_test.go`: `--prune` удаляет orphaned сервис после обновления compose

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

**Критерии приёмки:**
- [ ] `README.md` содержит: installation, quickstart, полный список команд и флагов, примеры
- [ ] `make release` создаёт бинарники для `linux/amd64`, `linux/arm64`, `darwin/arm64`
- [ ] GitHub Actions pipeline: test → lint → build → docker → release
- [ ] `go test -race ./...` проходит без data race
- [ ] `go vet ./...` без предупреждений

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
        │     │     └── internal/swarm/ (GetStackState)
        │     ├── internal/swarm/       (деплой в Swarm)
        │     │     └── internal/compose/ (конвертация spec)
        │     └── internal/health/      (мониторинг здоровья)
        │           └── internal/swarm/ (DockerClient)
        ├── cmd/rollback.go
        │     └── internal/swarm/
        ├── cmd/diff.go
        │     ├── internal/compose/
        │     ├── internal/swarm/   (GetStackState → StackState)
        │     └── internal/plan/    (CreatePlan, FormatPlan)
        ├── cmd/status.go
        │     └── internal/swarm/   (GetStackState + TaskList)
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
| 1 | CLI-интерфейс (скелет) | Высокий | 0 |
| 2 | Парсинг Compose | Высокий | 0 |
| 3 | Deployment ID | Средний | 0 |
| 4 | Утилиты путей | Низкий | 0 |
| 5а | DockerClient Abstraction | Критический | 0 |
| 5б | Состояние стека | Критический | 5а |
| 5в | Сети и тома | Высокий | 5а, 5б, 2 |
| 5г | Образы и сервисы | Критический | 5а, 5б, 2, 3 |
| 5д | Очистка ресурсов | Высокий | 5а, 5б |
| 5е | Оркестратор деплоя | Критический | 5б–5д |
| 6 | Снимки состояния | Высокий | 5е |
| 7 | Мониторинг здоровья | Критический | 5а |
| 8 | Планировщик (diff) | Средний | 2, 5б |
| 9 | Команда apply | Критический | 1, 2, 3, 4, 5е, 6, 7 |
| 10 | Команда rollback | Высокий | 1, 5е, 6 |
| 11 | Команда logs | Средний | 1, 5а |
| 12 | Команда events | Низкий | 1, 5а |
| 12а | Команда diff | Средний | 1, 2, 5б, 8 |
| 12б | Команда status | Средний | 1, 5б |
| 13 | Интеграционные тесты | Высокий | 9, 10, 11, 12а, 12б |
| 14 | Документация и релиз | Средний | все |

---

## MVP (Минимально жизнеспособный продукт)

Для первого рабочего релиза необходимы этапы: **0, 1, 2, 3, 4, 5а–5е, 6, 7, 9**

Это даёт:
- `stackman apply -n mystack -f docker-compose.yml`
- Деплой в Docker Swarm
- Ожидание healthy state
- Автоматический откат при ошибке
- Корректные коды выхода и логирование

**Полный рабочий инструмент** (все команды, включая diff/status): этапы MVP + **10, 11, 12, 12а, 12б**

---

## Оценка объёма кода

| Компонент | Файлов | Строк кода (ориентировочно) |
|-----------|--------|------------------------------|
| cmd/ (apply, rollback, logs, events, diff, status, version) | 8 | ~1000 |
| internal/compose/ | 5 | ~600 |
| internal/swarm/ (5а–5е) | 10 | ~1000 |
| internal/health/ | 5 | ~700 |
| internal/deployment/ | 2 | ~50 |
| internal/snapshot/ | 2 | ~200 |
| internal/plan/ | 4 | ~300 |
| internal/paths/ | 2 | ~80 |
| tests/ | 7 | ~700 |
| **Итого** | **~45** | **~4630** |

---

## Ключевые паттерны проектирования

1. **Interface-based abstraction** — `DockerClient` интерфейс для тестируемости; никакой прямой зависимости от Docker SDK вне `internal/swarm/`
2. **Event-driven architecture** — паттерн Watcher/Monitor для real-time обновлений состояния задач
3. **Goroutine orchestration** — `sync.WaitGroup` и каналы для параллельного мониторинга; отмена через общий `context`
4. **Context propagation** — таймауты и отмена через `context.Context` на всех уровнях стека
5. **Snapshot pattern** — захват состояния (`StackState`) перед деплоем для детерминированного отката
6. **Label-based filtering** — пространство имён стека через `com.docker.stack.namespace`; deploy tracking через `com.stackman.deploy.id`
7. **Idempotent resource management** — `EnsureNetworks`/`EnsureVolumes` не создают дубликаты при повторных вызовах
8. **Error wrapping** — все ошибки оборачиваются через `fmt.Errorf("context: %w", err)` для сохранения цепочки

---

*Документ создан: 2026-03-18*
*Версия: 2.0 — добавлены кросс-компонентные требования, критерии приёмки, разбивка Этапа 5, этапы 12а/12б*
