# Этап 14 — Документация и релиз

## Статус
🔴 Не начат

## Зависимости
- [x] Все предыдущие этапы завершены
- [x] `go test -race ./...` — зелёный
- [x] `go test -tags=integration ./tests/...` — зелёный

## Что создаём / изменяем
```
README.md                          # полная документация
.github/workflows/build.yml        # добавить release job
.github/workflows/release.yml      # pipeline для тегов
Makefile                           # цель release
docs/CONTRIBUTING.md               # гайд для контрибьюторов
```

---

## Шаг 1: `README.md`

Структура документа:

```markdown
# Stackman

> Zero-downtime Docker Swarm deployments with health monitoring and automatic rollback.

## Installation

### Binary
curl -sSL https://github.com/SomeBlackMagic/stackman/releases/latest/download/stackman-linux-amd64 \
  -o /usr/local/bin/stackman && chmod +x /usr/local/bin/stackman

### Docker
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/someblackmagic/stackman:latest --help

### Build from source
git clone https://github.com/SomeBlackMagic/stackman
cd stackman && make build

## Quick Start

# Инициализировать Swarm (если ещё не сделано)
docker swarm init

# Задеплоить стек
stackman apply -n myapp -f docker-compose.yml

# Посмотреть статус
stackman status -n myapp

# Откатить
stackman rollback -n myapp

## Commands

| Command    | Description                                      |
|------------|--------------------------------------------------|
| `apply`    | Deploy or update a stack                         |
| `rollback` | Roll back to previous state                      |
| `diff`     | Show planned changes without applying            |
| `status`   | Show current health of all stack services        |
| `logs`     | Stream logs from stack services                  |
| `events`   | Show Docker events for a stack                   |
| `version`  | Print version information                        |

## apply

stackman apply -n <stack> -f <compose-file> [flags]

Flags:
  -n, --name           Stack name (required)
  -f, --compose-file   Path to docker-compose.yml (required)
      --timeout        Deployment timeout (default: 5m)
      --prune          Remove services/networks not in compose file
      --no-logs        Disable log streaming during deploy
      --no-color       Disable colored output
      --log-level      Log level: debug|info|warn|error (default: info)
      --output         Output format: text|json (default: text)

Exit codes:
  0 — success
  1 — deploy error
  2 — timeout
  3 — interrupted, rollback succeeded
  4 — interrupted, rollback failed

## diff

stackman diff -n <stack> -f <compose-file>

Shows a terraform-style plan of what would change:
  + web       (create)
  ~ worker    (update)
    image: "myapp:1.0" → "myapp:1.1"
  - legacy    (delete)

## status

stackman status -n <stack>

NAME       IMAGE           REPLICAS  STATUS
web        nginx:1.25      3/3       running
worker     myapp:1.1       2/3       partial

## Environment Variables

| Variable            | Description                       |
|---------------------|-----------------------------------|
| DOCKER_HOST         | Docker daemon address              |
| DOCKER_TLS_VERIFY   | Enable TLS verification           |
| DOCKER_CERT_PATH    | Path to TLS certificates          |
| STACKMAN_TIMEOUT    | Default deployment timeout        |
| STACKMAN_LOG_LEVEL  | Default log level                 |
| STACKMAN_NO_COLOR   | Disable colored output (set to 1) |

## Troubleshooting

### Deploy hangs waiting for healthy state
Check container logs: stackman logs -n <stack>
Verify healthcheck command works inside container.
Use --timeout to set a shorter deadline.

### Rollback failed
Stack may be in inconsistent state.
Check Docker Swarm status: docker service ls
Manual recovery: docker service update --rollback <service>

### host-gateway not resolved
host-gateway resolution is only supported on single-node Swarm.
On multi-node clusters, use explicit IP addresses in extra_hosts.
```

---

## Шаг 2: Makefile — цель `release`

```makefile
DIST_DIR=dist

release: clean
	@mkdir -p $(DIST_DIR)
	@for GOOS in linux darwin; do \
	  for GOARCH in amd64 arm64; do \
	    OUTPUT=$(DIST_DIR)/stackman-$$GOOS-$$GOARCH; \
	    echo "Building $$OUTPUT..."; \
	    CGO_ENABLED=0 GOOS=$$GOOS GOARCH=$$GOARCH \
	      go build $(LDFLAGS) -o $$OUTPUT .; \
	  done; \
	done
	@ls -lh $(DIST_DIR)/

clean:
	rm -rf $(DIST_DIR) stackman
```

**Верификация:**
```bash
make release
ls -lh dist/
# stackman-linux-amd64
# stackman-linux-arm64
# stackman-darwin-amd64
# stackman-darwin-arm64
```

---

## Шаг 3: GitHub Actions — release pipeline

**`.github/workflows/release.yml`:**
```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      packages: write

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'

      - name: Run tests
        run: make test

      - name: Build release binaries
        run: |
          VERSION=${GITHUB_REF_NAME}
          COMMIT=${GITHUB_SHA::8}
          BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
          make release \
            VERSION=$VERSION \
            COMMIT=$COMMIT \
            BUILD_DATE=$BUILD_DATE

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Login to GitHub Container Registry
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build and push Docker image
        uses: docker/build-push-action@v5
        with:
          platforms: linux/amd64,linux/arm64
          push: true
          tags: |
            ghcr.io/someblackmagic/stackman:${{ github.ref_name }}
            ghcr.io/someblackmagic/stackman:latest
          build-args: |
            VERSION=${{ github.ref_name }}
            COMMIT=${{ github.sha }}
            BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

      - name: Create GitHub Release
        uses: softprops/action-gh-release@v1
        with:
          files: dist/*
          generate_release_notes: true
```

---

## Шаг 4: Обновить `build.yml` — добавить `go vet` и race detector

```yaml
# .github/workflows/build.yml — обновить секцию test:
- name: Unit tests (with race detector)
  run: go test -race ./cmd/... ./internal/...

- name: go vet
  run: go vet ./...

- name: Integration tests
  run: |
    docker swarm init
    make test-integration
    docker swarm leave --force
```

---

## Шаг 5: Финальные проверки

Выполнить перед созданием релизного тега:

```bash
# 1. Все unit-тесты
go test -race ./...

# 2. go vet
go vet ./...

# 3. Сборка для всех платформ
make release

# 4. Проверка версии в бинаре
./dist/stackman-linux-amd64 version

# 5. Интеграционные тесты (требует Docker Swarm)
docker swarm init
make test-integration
docker swarm leave --force

# 6. Проверка Docker образа
docker buildx build --platform linux/amd64 -t stackman:test --load .
docker run --rm stackman:test version
```

---

## Чеклист релиза

- [ ] `go test -race ./...` — зелёный
- [ ] `go vet ./...` — без предупреждений
- [ ] `go mod tidy` — не изменяет файлы
- [ ] `make release` создаёт бинари для linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
- [ ] `./dist/stackman-linux-amd64 version` выводит корректную версию
- [ ] Docker образ собирается для `linux/amd64,linux/arm64`
- [ ] `make test-integration` зелёный на чистом Docker Swarm
- [ ] `README.md` содержит installation, quickstart, все команды, troubleshooting
- [ ] GitHub Actions release pipeline срабатывает на тег `v*`
- [ ] После тега: release assets загружены, Docker образ запушен

## Создание релиза

```bash
git tag v1.0.0
git push origin v1.0.0
# GitHub Actions автоматически создаст release
```
