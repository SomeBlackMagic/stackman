# Этап 0 — Скелет проекта

## Статус
🔴 Не начат

## Зависимости
Нет. Это первый этап.

## Что создаём
```
stackman/
├── main.go
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
├── .github/workflows/build.yml
├── .gitignore
├── LICENSE
├── README.md
└── cmd/        (пустая директория с .gitkeep)
└── internal/   (пустая директория с .gitkeep)
└── tests/      (пустая директория с .gitkeep)
└── docs/       (уже содержит DEVELOPMENT_ROADMAP.md)
```

---

## TDD-план

> На этом этапе unit-тестов нет — только инфраструктура.
> Каждый шаг верифицируется конкретной командой.

---

### Шаг 1: Инициализация Go-модуля

```bash
go mod init github.com/SomeBlackMagic/stackman
```

**Верификация:**
```bash
cat go.mod
# module github.com/SomeBlackMagic/stackman
# go 1.24.0
```

---

### Шаг 2: Точка входа `main.go`

```go
// main.go
package main

import "fmt"

func main() {
    fmt.Println("stackman")
}
```

**Верификация:**
```bash
go build -o stackman .
./stackman
# stackman
```

---

### Шаг 3: Структура директорий

```bash
mkdir -p cmd internal tests docs
touch cmd/.gitkeep internal/.gitkeep tests/.gitkeep
```

---

### Шаг 4: `Makefile`

Цели, которые должны работать уже сейчас:

```makefile
BINARY_NAME=stackman
VERSION?=dev
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildDate=$(BUILD_DATE)"

.PHONY: build test lint clean docker-build

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) .

test:
	go test -race ./...

lint:
	go vet ./...

clean:
	rm -f $(BINARY_NAME)

docker-build:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-t stackman:$(VERSION) .
```

**Верификация:**
```bash
make build    # собирается
make test     # no test files — OK (exit 0)
make lint     # no issues
```

---

### Шаг 5: `Dockerfile` (multi-stage, multi-arch)

```dockerfile
# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE} -s -w" \
    -o stackman .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/stackman /usr/local/bin/stackman
ENTRYPOINT ["stackman"]
```

**Верификация:**
```bash
docker buildx build --platform linux/amd64 -t stackman:test --load .
docker run --rm stackman:test
```

---

### Шаг 6: `.github/workflows/build.yml`

```yaml
name: Build & Test

on:
  push:
    branches: [main, "claude/**"]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - run: go mod download
      - run: make test
      - run: make lint

  docker:
    runs-on: ubuntu-latest
    needs: test
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3
      - run: make docker-build
```

---

### Шаг 7: Зависимости

```bash
go get github.com/docker/docker@v28.0.0
go get gopkg.in/yaml.v3
go mod tidy
```

**Верификация:**
```bash
go build ./...  # без ошибок
```

---

### Шаг 8: `.gitignore`

```gitignore
stackman
*.test
*.out
dist/
.env
```

---

## Критерии завершения этапа

- [ ] `go build ./...` — без ошибок
- [ ] `./stackman` — запускается, выводит "stackman"
- [ ] `make build test lint` — все три зелёные
- [ ] `docker buildx build --platform linux/amd64,linux/arm64 .` — собирается
- [ ] `go mod tidy` — не изменяет файлы
- [ ] GitHub Actions pipeline проходит

## Следующий этап
→ [Этап 1: CLI-скелет](./01-cli-skeleton.md)
