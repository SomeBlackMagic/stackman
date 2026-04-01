# Этап 3 — Deployment ID (`internal/deployment/`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 0: скелет проекта

## Что создаём
```
internal/deployment/
├── id.go
└── id_test.go
```

---

## TDD-план

### Цикл 1: Формат ID

**🔴 Тест:**
```go
// internal/deployment/id_test.go
package deployment_test

import (
    "regexp"
    "testing"
    "github.com/SomeBlackMagic/stackman/internal/deployment"
)

var deployIDPattern = regexp.MustCompile(`^deploy-\d{8}-\d{6}-[0-9a-f]{8}$`)

func TestGenerateID_Format(t *testing.T) {
    id := deployment.GenerateDeployID()
    if !deployIDPattern.MatchString(id) {
        t.Errorf("invalid deploy ID format: %q\nexpected: deploy-YYYYMMDD-HHMMSS-RANDOM8HEX", id)
    }
}
```

**🟢 Реализация:**
```go
// internal/deployment/id.go
package deployment

import (
    "crypto/rand"
    "fmt"
    "time"
)

const DeployIDLabel = "com.stackman.deploy.id"

func GenerateDeployID() string {
    b := make([]byte, 4)
    if _, err := rand.Read(b); err != nil {
        // rand.Read никогда не возвращает ошибку на поддерживаемых платформах
        panic(fmt.Sprintf("deployment: failed to generate random bytes: %v", err))
    }
    return fmt.Sprintf("deploy-%s-%x", time.Now().UTC().Format("20060102-150405"), b)
}
```

---

### Цикл 2: Уникальность

**🔴 Тест:**
```go
func TestGenerateID_Unique(t *testing.T) {
    seen := make(map[string]bool)
    for i := 0; i < 100; i++ {
        id := deployment.GenerateDeployID()
        if seen[id] {
            t.Fatalf("duplicate deploy ID: %q", id)
        }
        seen[id] = true
    }
}
```

Тест пройдёт автоматически за счёт случайных байт.

---

### Цикл 3: Константа метки

**🔴 Тест:**
```go
func TestDeployIDLabel(t *testing.T) {
    want := "com.stackman.deploy.id"
    if deployment.DeployIDLabel != want {
        t.Errorf("expected label %q, got %q", want, deployment.DeployIDLabel)
    }
}
```

Тест пройдёт — константа уже определена.

---

## Критерии завершения этапа

- [ ] Формат: `^deploy-\d{8}-\d{6}-[0-9a-f]{8}$`
- [ ] 100 вызовов подряд — все разные
- [ ] `DeployIDLabel == "com.stackman.deploy.id"`
- [ ] `go test ./internal/deployment/...` — зелёный

## Следующий этап
→ [Этап 4: Утилиты путей](./04-paths.md)
