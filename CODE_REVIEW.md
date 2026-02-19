# Отчёт по анализу кодовой базы Stackman

## Контекст

Проект Stackman — CLI-инструмент для оркестрации деплоя Docker Swarm стеков.
Структура: `cmd/` (транспорт/CLI) → `internal/swarm/` (инфраструктура) → `internal/health/`, `internal/compose/`, `internal/plan/` (доменная/бизнес-логика).

---

## Нарушения по правилам

### Архитектура

**Правило 2 — UseCase/domain слой должен зависеть только от интерфейсов**

`internal/health/watcher.go` и `internal/health/monitor.go` принимают `client.APIClient` (конкретный тип из Docker SDK) напрямую, а не через кастомный интерфейс. Это делает health-пакет жёстко связанным с библиотекой Docker.

```go
// health/monitor.go — нарушение
func NewMonitor(client client.APIClient, ...) *Monitor { ... }
func NewMonitorWithLogs(client client.APIClient, ...) *Monitor { ... }
```

**Правило 3 — Интерфейсы должны быть объявлены в том слое, который их потребляет**

Интерфейс `DockerClient` объявлен в `internal/swarm/interface.go` (инфраструктурный пакет). Пакет `health` вместо использования этого интерфейса использует конкретный `client.APIClient`. Каждый потребляющий пакет должен объявлять свой минимальный интерфейс.

**Правило 7 — Глобальные переменные запрещены**

`cmd/version.go` содержит глобальные переменные пакетного уровня:
```go
var (
    version  string = "dev"
    revision string = "000000000000000000000000000000"
)
```

---

### Код и стиль

**Правило 12 — Ошибки должны оборачиваться через `%w`**

В `internal/health/service_update_monitor.go` ошибки формируются без цепочки:
```go
// Нарушение — нет оригинального контекста ошибки
return fmt.Errorf("service update paused: %s", message)
return fmt.Errorf("service update rolled back: %s", message)
```

**Правило 16 — Доменный слой не должен выполнять логирование**

`log.Printf` используется напрямую в пакетах бизнес-логики:
- `internal/swarm/services.go`
- `internal/swarm/rollback.go`
- `internal/health/monitor.go`
- `internal/health/watcher.go`

**Правило 17 — Logger должен быть инжектирован как зависимость**

Во всём проекте используется пакет `log` напрямую без инжекции. Логгер нигде не передаётся через конструкторы. Это нарушает принципы DI и затрудняет тестирование.

**Правило 18 — Интерфейсы должны быть минимальными**

`DockerClient` в `internal/swarm/interface.go` содержит 21 метод. Такой "жирный" интерфейс нарушает ISP. Его следует разбить на несколько узкоспециализированных интерфейсов.

---

### Горутины и конкурентность

**Правило 22 — Fire-and-forget горутины запрещены**

В `cmd/apply.go` горутина `monitorServiceTasks` запускается без WaitGroup:
```go
go monitorServiceTasks(ctx, cli, svc, serviceEventsChan, opts.ShowLogs, deployResult.DeployID)
// не отслеживается, завершение не ожидается
```

В `internal/health/watcher.go` горутины запускаются без WaitGroup:
```go
go w.broadcastEvents(ctx)  // fire-and-forget
go w.pollTasks(ctx)        // fire-and-forget
```

**Правило 25/26 — Конкурентность должна быть ограничена; неограниченный fan-out запрещён**

В `cmd/apply.go`, функция `waitForAllTasksHealthy` запускает горутину на каждый контейнер без ограничений:
```go
for _, ct := range containersToInspect {
    wg.Add(1)
    go func(c containerTask) { // неограниченный fan-out
        defer wg.Done()
        info, err := cli.ContainerInspect(ctx, c.containerID)
        resultChan <- inspectResult{ct: c, info: info, err: err}
    }(ct)
}
```

**Правило 27 — Использовать `errgroup` для оркестрации конкурентных задач**

Проект использует `sync.WaitGroup` + каналы ошибок вместо `golang.org/x/sync/errgroup`. Паттерн с errgroup компактнее и безопаснее.

---

### Горутины без внешнего контроля и остановки

Полный обзор всех 14 горутин в проекте (11 мест запуска):

| Место | Горутина | WaitGroup | Контекст | Fire-forget | Риск утечки |
|-------|----------|-----------|----------|-------------|-------------|
| `apply.go:144` | Signal handler | — | Частично | Да | Низкий |
| `apply.go:197` | Service update monitor | **Да** | **Да** | Нет | Нет |
| `apply.go:216` | Service watcher | **Нет** | Да | **Да** | Средний |
| `apply.go:223` | Task monitor | **Нет** | Да | **Да** | Средний |
| `apply.go:229` | WaitGroup closer | Да | — | Да | Нет |
| `apply.go:314` | Task monitor runner | **Нет** | Да | **Да** | Низкий |
| `apply.go:497` | Container inspection | **Да** | **Да** | Нет | Нет |
| `apply.go:504` | Result closer | Да | — | Да | Нет |
| `watcher.go:113` | broadcastEvents | **Нет** | Да | **Да** | Низкий |
| `watcher.go:117` | pollTasks | **Нет** | Да | **Да** | Низкий |
| `watcher.go:619` | Service filter | **Нет** | Неявно | **Да** | **Критический** |
| `monitor.go:85,93,103` | Monitor (3 шт.) | **Да** | **Да** | Нет | Нет |

#### Детали по проблемным местам

**`watcher.go:619` — КРИТИЧЕСКИЙ риск утечки**

`SubscribeToService()` запускает горутину-фильтр, которая останавливается **только** если вызвать возвращённую функцию `unsubscribe()`. Если вызывающий код забудет её вызвать (или паника до вызова), горутина живёт вечно:

```go
// watcher.go:619
go func() {
    defer close(filtered)
    defer w.Unsubscribe(allEvents) // cleanup родительской подписки
    for {
        select {
        case <-done:        // единственный способ остановки — вызов unsubscribe()
            return
        case event, ok := <-allEvents:
            // ... логика фильтрации ...
        }
    }
}()
```

Нет привязки к context — даже после отмены контекста горутина продолжит работу, если `unsubscribe()` не вызван.

**`apply.go:216` и `apply.go:223` — нет WaitGroup**

Service watcher и task monitor запускаются fire-and-forget без отслеживания:

```go
// apply.go:216 — нет wg.Add/Done
go func(w *health.Watcher, svcName string) {
    if err := w.Start(ctx); err != nil && err != context.Canceled {
        log.Printf(...)
    }
}(serviceWatcher, svc.ServiceName)

// apply.go:223 — нет wg.Add/Done
go monitorServiceTasks(ctx, cli, svc, serviceEventsChan, opts.ShowLogs, deployResult.DeployID)
```

Основная функция завершается без ожидания этих горутин → возможна паника при обращении к уже освобождённым ресурсам.

**`watcher.go:113` и `watcher.go:117` — нет WaitGroup**

`watcher.Start()` запускает `broadcastEvents` и `pollTasks`, но не ждёт их завершения:

```go
// watcher.go:113
go w.broadcastEvents(ctx)
// watcher.go:117
go w.pollTasks(ctx)
// Start() возвращает nil после select{<-ctx.Done()}, горутины ещё могут работать
```

**`apply.go:314` — self-cleaning, но без WaitGroup**

Горутина самостоятельно удаляет себя из map по завершению, но не отслеживается родителем:

```go
go func(m *health.Monitor) {
    if err := m.Start(ctx); err != nil && err != context.Canceled { ... }
    mu.Lock()
    delete(taskMonitors, taskID) // само-очистка
    mu.Unlock()
}(monitor)
```

---

### Жизненный цикл приложения

**Правило 41 — Приложение должно поддерживать graceful shutdown**

Обработчик сигналов в `cmd/apply.go` использует `os.Exit()` вместо graceful shutdown:
```go
go func() {
    sig := <-sigChan
    // ... rollback ...
    os.Exit(130) // принудительное завершение — не graceful
}()
```
`os.Exit()` обходит все отложенные `defer` и не позволяет горутинам корректно завершиться.

**Правило 43 — Завершение горутин должно ожидаться через WaitGroup или errgroup**

Горутины `broadcastEvents` и `pollTasks` в `watcher.go` запущены без механизма ожидания. `watcher.Start()` возвращает управление, не дожидаясь завершения этих горутин.

**Правило 49 — Panic внутри горутин должен быть перехвачен и залогирован**

В проекте нет ни одного `recover()`. Любая паника в горутинах `monitorServiceTasks`, `broadcastEvents`, `pollTasks` и других приведёт к крашу приложения без диагностики.

---

### Тестирование и надёжность

**Правило 52 — Использовать табличные тесты**

Тесты в `internal/health/monitor_test.go` написаны в виде последовательных вызовов, не через `[]struct{...}` с циклом. Пример:
```go
func TestMonitor_HandleEvent(t *testing.T) {
    // 30+ строк последовательных проверок вместо таблицы
}
```

---

## Сводная таблица нарушений

| # | Правило | Файл(ы) | Критичность |
|---|---------|---------|-------------|
| 2 | UseCase зависит от конкретной реализации | `health/monitor.go`, `health/watcher.go` | Высокая |
| 3 | Интерфейс объявлен не в потребляющем пакете | `swarm/interface.go` | Средняя |
| 7 | Глобальные переменные | `cmd/version.go` | Низкая |
| 12 | Ошибки не оборачиваются через `%w` | `health/service_update_monitor.go` | Средняя |
| 16 | Логирование в доменном слое | `swarm/`, `health/` | Средняя |
| 17 | Logger не инжектируется | Весь проект | Средняя |
| 18 | Интерфейс избыточно большой (21 метод) | `swarm/interface.go` | Средняя |
| 22 | Fire-and-forget горутины | `cmd/apply.go`, `health/watcher.go` | Высокая |
| 25/26 | Неограниченный fan-out | `cmd/apply.go:497` | Высокая |
| 27 | Не используется errgroup | `cmd/apply.go` | Низкая |
| 41 | os.Exit вместо graceful shutdown | `cmd/apply.go` | Высокая |
| 43 | Горутины не ожидаются (WaitGroup) | `health/watcher.go:113,117`, `cmd/apply.go:216,223` | Высокая |
| 44 | Утечка горутины без привязки к context | `health/watcher.go:619` (SubscribeToService) | Критическая |
| 46 | Горутина на каждый контейнер без лимита | `cmd/apply.go:497` | Высокая |
| 49 | Нет recover() в горутинах | Весь проект | Высокая |
| 52 | Нет табличных тестов | `health/monitor_test.go` | Низкая |

---

## Приоритет исправлений

### Критические (немедленно)
1. **`watcher.go:619` — привязать `SubscribeToService` к контексту**: горутина-фильтр должна завершаться при отмене `ctx`, а не только при явном вызове `unsubscribe()`. Добавить `case <-ctx.Done(): return` в select-блок горутины
2. Добавить `recover()` во все долгоживущие горутины
3. Устранить fire-and-forget: добавить WaitGroup для `monitorServiceTasks` (`apply.go:223`), service watcher (`apply.go:216`), `broadcastEvents` и `pollTasks` (`watcher.go:113,117`)
4. Ограничить fan-out при инспекции контейнеров (семафор / worker pool) — `apply.go:497`
5. Заменить `os.Exit()` в обработчике сигналов на корректный cancel + ожидание горутин

### Важные
6. Ввести интерфейс для Docker-клиента в пакете `health` (убрать зависимость от `client.APIClient`)
7. Инжектировать логгер через конструкторы вместо глобального `log`
8. Дообернуть ошибки через `%w` в `service_update_monitor.go`

### Желательные
9. Разбить `DockerClient` на узкие интерфейсы
10. Убрать глобальные переменные из `version.go`
11. Переписать тесты в table-driven стиле
12. Рассмотреть переход на `errgroup` для оркестрации

---

## Что сделано хорошо

- Context передаётся первым аргументом везде
- Context не хранится в структурах
- Shared state защищён mutex/RWMutex
- Каналы буферизованы
- `sync.Once` для idempotent shutdown
- `DockerClient` interface существует для инфраструктурного слоя
- Retry с backoff в `swarm/services.go`
- Mock для тестирования реализован
