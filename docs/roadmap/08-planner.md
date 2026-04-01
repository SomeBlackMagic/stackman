# Этап 8 — Планировщик деплоя (`internal/plan/`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 2: compose.ComposeFile
- [x] Этап 5б: swarm.StackState

## Что создаём
```
internal/plan/
├── types.go
├── planner.go
├── formatter.go
└── planner_test.go
```

---

## TDD-план

### Цикл 1: Пустой план для идентичного состояния

**🔴 Тест:**
```go
// internal/plan/planner_test.go
package plan_test

import (
    "testing"

    "github.com/SomeBlackMagic/stackman/internal/compose"
    "github.com/SomeBlackMagic/stackman/internal/plan"
    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
    "github.com/docker/docker/api/types/swarm"
)

func TestCreatePlan_NoChanges(t *testing.T) {
    current := &swarmint.StackState{
        Services: map[string]swarm.Service{
            "web": {Spec: swarm.ServiceSpec{
                Annotations: swarm.Annotations{Name: "mystack_web"},
                TaskTemplate: swarm.TaskSpec{
                    ContainerSpec: &swarm.ContainerSpec{Image: "nginx:latest"},
                },
            }},
        },
        Networks: map[string]swarm.Network{},
        Volumes:  map[string]swarm.Volume{},
    }
    desired := &compose.ComposeFile{
        Services: map[string]compose.Service{
            "web": {Image: "nginx:latest"},
        },
    }
    p, err := plan.CreatePlan(current, desired, "mystack")
    if err != nil {
        t.Fatal(err)
    }
    if !p.IsEmpty() {
        t.Errorf("expected empty plan, got: %+v", p)
    }
}
```

**🟢 Реализация `types.go`:**
```go
// internal/plan/types.go
package plan

import "github.com/docker/docker/api/types/swarm"

type ActionType string

const (
    ActionCreate ActionType = "create"
    ActionUpdate ActionType = "update"
    ActionDelete ActionType = "delete"
    ActionNone   ActionType = "none"
)

type ServiceAction struct {
    Name        string
    ServiceID   string
    Action      ActionType
    CurrentSpec *swarm.ServiceSpec
    DesiredSpec *swarm.ServiceSpec
    Changes     []string // человекочитаемые описания изменений
}

type NetworkAction struct {
    Name   string
    Action ActionType
}

type VolumeAction struct {
    Name   string
    Action ActionType
}

type Plan struct {
    StackName string
    Services  []ServiceAction
    Networks  []NetworkAction
    Volumes   []VolumeAction
}

// IsEmpty возвращает true если нет никаких изменений.
func (p *Plan) IsEmpty() bool {
    for _, s := range p.Services {
        if s.Action != ActionNone {
            return false
        }
    }
    for _, n := range p.Networks {
        if n.Action != ActionNone {
            return false
        }
    }
    for _, v := range p.Volumes {
        if v.Action != ActionNone {
            return false
        }
    }
    return true
}
```

**Реализация `planner.go`:**
```go
// internal/plan/planner.go
package plan

import (
    "github.com/SomeBlackMagic/stackman/internal/compose"
    swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

// CreatePlan сравнивает текущее состояние стека с желаемым.
func CreatePlan(current *swarmint.StackState, desired *compose.ComposeFile, stackName string) (*Plan, error) {
    p := &Plan{StackName: stackName}
    p.Services = planServices(current, desired)
    p.Networks = planNetworks(current, desired)
    p.Volumes = planVolumes(current, desired)
    return p, nil
}

func planServices(current *swarmint.StackState, desired *compose.ComposeFile) []ServiceAction {
    var actions []ServiceAction

    // Сервисы для создания или обновления
    for name, svc := range desired.Services {
        svc := svc
        desiredSpec, _ := compose.ConvertToSwarmSpec(name, svc, "")
        if existing, ok := current.Services[name]; ok {
            // Сравниваем образы как простейший diff
            changes := diffServiceSpec(existing.Spec, desiredSpec)
            if len(changes) > 0 {
                actions = append(actions, ServiceAction{
                    Name:        name,
                    ServiceID:   existing.ID,
                    Action:      ActionUpdate,
                    CurrentSpec: &existing.Spec,
                    DesiredSpec: &desiredSpec,
                    Changes:     changes,
                })
            } else {
                actions = append(actions, ServiceAction{Name: name, Action: ActionNone})
            }
        } else {
            actions = append(actions, ServiceAction{
                Name:        name,
                Action:      ActionCreate,
                DesiredSpec: &desiredSpec,
            })
        }
    }

    // Сервисы для удаления (есть в current, нет в desired)
    for name, svc := range current.Services {
        if _, ok := desired.Services[name]; !ok {
            svcCopy := svc.Spec
            actions = append(actions, ServiceAction{
                Name:        name,
                ServiceID:   svc.ID,
                Action:      ActionDelete,
                CurrentSpec: &svcCopy,
            })
        }
    }
    return actions
}

// diffServiceSpec возвращает список изменений между spec'ами.
// Минимальная реализация: сравниваем только образ.
func diffServiceSpec(current, desired swarm.ServiceSpec) []string {
    var changes []string
    if current.TaskTemplate.ContainerSpec != nil &&
        desired.TaskTemplate.ContainerSpec != nil {
        if current.TaskTemplate.ContainerSpec.Image != desired.TaskTemplate.ContainerSpec.Image {
            changes = append(changes, fmt.Sprintf("image: %q → %q",
                current.TaskTemplate.ContainerSpec.Image,
                desired.TaskTemplate.ContainerSpec.Image))
        }
    }
    return changes
}

func planNetworks(current *swarmint.StackState, desired *compose.ComposeFile) []NetworkAction {
    var actions []NetworkAction
    for name := range desired.Networks {
        if _, ok := current.Networks[name]; ok {
            actions = append(actions, NetworkAction{Name: name, Action: ActionNone})
        } else {
            actions = append(actions, NetworkAction{Name: name, Action: ActionCreate})
        }
    }
    for name := range current.Networks {
        if _, ok := desired.Networks[name]; !ok {
            actions = append(actions, NetworkAction{Name: name, Action: ActionDelete})
        }
    }
    return actions
}

func planVolumes(current *swarmint.StackState, desired *compose.ComposeFile) []VolumeAction {
    var actions []VolumeAction
    for name := range desired.Volumes {
        if _, ok := current.Volumes[name]; ok {
            actions = append(actions, VolumeAction{Name: name, Action: ActionNone})
        } else {
            actions = append(actions, VolumeAction{Name: name, Action: ActionCreate})
        }
    }
    for name := range current.Volumes {
        if _, ok := desired.Volumes[name]; !ok {
            actions = append(actions, VolumeAction{Name: name, Action: ActionDelete})
        }
    }
    return actions
}
```

---

### Цикл 2: Create для нового сервиса

**🔴 Тест:**
```go
func TestCreatePlan_NewService(t *testing.T) {
    current := &swarmint.StackState{
        Services: map[string]swarm.Service{},
        Networks: map[string]swarm.Network{},
        Volumes:  map[string]swarm.Volume{},
    }
    desired := &compose.ComposeFile{
        Services: map[string]compose.Service{"api": {Image: "myapi:1.0"}},
    }
    p, _ := plan.CreatePlan(current, desired, "mystack")
    if len(p.Services) != 1 || p.Services[0].Action != plan.ActionCreate {
        t.Errorf("expected 1 create action, got: %+v", p.Services)
    }
    if p.IsEmpty() {
        t.Error("plan should not be empty")
    }
}
```

---

### Цикл 3: Delete для удалённого сервиса

**🔴 Тест:**
```go
func TestCreatePlan_DeletedService(t *testing.T) {
    current := &swarmint.StackState{
        Services: map[string]swarm.Service{
            "legacy": {ID: "svc1", Spec: swarm.ServiceSpec{}},
        },
        Networks: map[string]swarm.Network{},
        Volumes:  map[string]swarm.Volume{},
    }
    desired := &compose.ComposeFile{Services: map[string]compose.Service{}}
    p, _ := plan.CreatePlan(current, desired, "mystack")
    found := false
    for _, s := range p.Services {
        if s.Name == "legacy" && s.Action == plan.ActionDelete {
            found = true
        }
    }
    if !found {
        t.Error("expected delete action for 'legacy'")
    }
}
```

---

### Цикл 4: Форматирование плана

**🔴 Тест:**
```go
// internal/plan/formatter_test.go
func TestFormatPlan_PrefixSymbols(t *testing.T) {
    p := &plan.Plan{
        StackName: "mystack",
        Services: []plan.ServiceAction{
            {Name: "new-svc", Action: plan.ActionCreate},
            {Name: "changed", Action: plan.ActionUpdate, Changes: []string{"image: old → new"}},
            {Name: "removed", Action: plan.ActionDelete},
            {Name: "same", Action: plan.ActionNone},
        },
    }
    output := plan.FormatPlan(p)

    if !strings.Contains(output, "+ new-svc") {
        t.Errorf("create should have + prefix")
    }
    if !strings.Contains(output, "~ changed") {
        t.Errorf("update should have ~ prefix")
    }
    if !strings.Contains(output, "- removed") {
        t.Errorf("delete should have - prefix")
    }
    if strings.Contains(output, "same") {
        t.Errorf("no-op services should not appear in output")
    }
}

func TestFormatPlan_NoChanges(t *testing.T) {
    p := &plan.Plan{StackName: "mystack"}
    output := plan.FormatPlan(p)
    if !strings.Contains(output, "No changes") {
        t.Errorf("empty plan should output 'No changes', got: %q", output)
    }
}
```

**🟢 Реализация `formatter.go`:**
```go
// internal/plan/formatter.go
package plan

import (
    "fmt"
    "strings"
)

func FormatPlan(p *Plan) string {
    if p.IsEmpty() {
        return "No changes. Stack is up to date.\n"
    }

    var sb strings.Builder
    fmt.Fprintf(&sb, "Plan for stack %q:\n\n", p.StackName)

    for _, s := range p.Services {
        switch s.Action {
        case ActionCreate:
            fmt.Fprintf(&sb, "  + %s (create)\n", s.Name)
        case ActionUpdate:
            fmt.Fprintf(&sb, "  ~ %s (update)\n", s.Name)
            for _, c := range s.Changes {
                fmt.Fprintf(&sb, "      %s\n", c)
            }
        case ActionDelete:
            fmt.Fprintf(&sb, "  - %s (delete)\n", s.Name)
        }
    }

    creates, updates, deletes := 0, 0, 0
    for _, s := range p.Services {
        switch s.Action {
        case ActionCreate: creates++
        case ActionUpdate: updates++
        case ActionDelete: deletes++
        }
    }
    fmt.Fprintf(&sb, "\nPlan: %d to add, %d to change, %d to destroy.\n", creates, updates, deletes)
    return sb.String()
}
```

---

## Критерии завершения этапа

- [ ] Идентичное состояние → `IsEmpty() == true`
- [ ] Новый сервис → `ActionCreate`
- [ ] Удалённый сервис → `ActionDelete`
- [ ] Изменённый образ → `ActionUpdate` с описанием изменения
- [ ] `FormatPlan` — `+` для create, `~` для update, `-` для delete
- [ ] `FormatPlan` для пустого плана → "No changes"
- [ ] `go test ./internal/plan/...` — зелёный

## Следующий этап
→ [Этап 9: Команда apply](./09-apply-command.md)
