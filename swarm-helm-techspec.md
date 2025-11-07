# Техническое задание: CLI-утилита "swarm-helm"

## Цель
CLI-утилита на Go, аналог Helm для Kubernetes, но для Docker Swarm:
- Читает `docker-compose.yml`.
- Создает, обновляет, удаляет сервисы и ресурсы стека через Docker Socket API.
- Ждет успешного обновления (healthy) всех сервисов.
- Поддерживает откат (rollback) при неудаче или сигнале завершения.

## Зависимости
Только следующие пакеты:
```
github.com/docker/docker v28.5.1+incompatible
github.com/docker/go-units v0.5.0
golang.org/x/net v0.43.0
gopkg.in/yaml.v3 v3.0.1
```
Парсинг `docker-compose.yml` выполняется вручную средствами утилиты без внешних библиотек.

## Ограничения
- Запрещено использование подпроцессов (`exec.Command` и т.п.).
- Вся работа выполняется через Docker Socket API.

## Авторизация и Docker Config
Утилита должна принимать переменную окружения `DOCKER_CONFIG_PATH`, указывающую на каталог, где находится `config.json` с авторизационными данными для Docker Registry.

## Обработка путей для volume
При создании volume все относительные пути должны быть преобразованы в абсолютные на основе текущей директории (`os.Getwd()`) или переменной `SWARM_STACK_PATH`.

Пример преобразования:
```go
// Convert relative paths to absolute paths
if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
    source = strings.Replace(source, "./", cwd+"/", 1)
    if strings.HasPrefix(parts[0], "../") {
        source = cwd + "/" + parts[0]
    }
} else if !strings.HasPrefix(source, "/") && !strings.Contains(source, "://") {
    source = cwd + "/" + source
}
```

## Основные функции
1. **Парсинг `docker-compose.yml`**
   - Преобразует структуру в Swarm ServiceSpec.
2. **Деплой и обновление**
   - Через Docker API: `ServiceCreate`, `ServiceUpdate`, `ServiceRemove`.
   - Управление сетями, конфигами, секретами, volume.
3. **Мониторинг и ожидание healthy**
   - Подписка на события Docker (`Events`).
   - Мониторинг `Task` и `ContainerInspect` для проверки состояния health.
4. **Откат (rollback)**
   - Снимок текущего состояния перед обновлением.
   - Восстановление сервисов и ресурсов из снимка при сбое.
5. **Обработка сигналов SIGINT/SIGTERM**
   - Прерывает ожидание healthy и инициирует откат.

## Таймауты
- Общий таймаут релиза — 15 минут (по умолчанию).
- Таймаут health задачи — 5 минут.
- Таймаут отката — 10 минут.

## Выходные коды
- `0` — успешный релиз или успешный откат.
- `2` — релиз не завершен по таймауту, откат успешен.
- `3` — релиз не завершен, откат неуспешен.
- `4` — ошибка соединения с Docker API.

## Переменные окружения
- `DOCKER_CONFIG_PATH` — путь к каталогу с `config.json`.
- `SWARM_STACK_PATH` — базовый путь для относительных путей.
- `SWARM_HELM_TIMEOUT` — таймаут релиза.
- `SWARM_HELM_ROLLBACK_TIMEOUT` — таймаут отката.

## Архитектура
- `pkg/compose` — парсинг `docker-compose.yml`.
- `pkg/swarm` — клиент Docker API.
- `pkg/apply` — логика деплоя/обновления/удаления.
- `pkg/health` — контроль состояния сервисов и контейнеров.
- `pkg/snapshot` — хранение и восстановление снапшотов.
- `pkg/rollback` — логика отката.
- `pkg/signals` — обработка сигналов.
- `pkg/output` — форматирование и вывод статуса.

## Тестирование
- Unit-тесты для парсера Compose.
- Интеграционные тесты: создание, обновление и откат сервисов.
