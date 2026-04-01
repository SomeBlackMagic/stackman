# Этап 2 — Парсинг Docker Compose (`internal/compose/`)

## Статус
🔴 Не начат

## Зависимости
- [x] Этап 0: скелет проекта

## Что создаём
```
internal/compose/
├── types.go
├── parser.go
├── converter.go
├── extra_hosts.go
├── parser_test.go
├── converter_test.go
└── extra_hosts_test.go

tests/testdata/
├── minimal.yml
├── full-service.yml
└── invalid.yml
```

---

## TDD-план

### Цикл 1: Парсер — несуществующий файл

**🔴 Тест:**
```go
// internal/compose/parser_test.go
package compose_test

import (
    "testing"
    "github.com/SomeBlackMagic/stackman/internal/compose"
)

func TestParseFile_NotFound(t *testing.T) {
    _, err := compose.ParseFile("/nonexistent/file.yml")
    if err == nil {
        t.Fatal("expected error for nonexistent file")
    }
}
```

**🟢 Реализация:**
```go
// internal/compose/parser.go
package compose

import "fmt"

func ParseFile(path string) (*ComposeFile, error) {
    return nil, fmt.Errorf("not implemented")
}
```

```go
// internal/compose/types.go
package compose

type ComposeFile struct{}
```

Тест проходит — ошибка возвращается (пусть и не та).

---

### Цикл 2: Парсер — минимальный валидный файл

**Создаём testdata:**
```yaml
# internal/compose/testdata/minimal.yml
version: "3.8"
services:
  web:
    image: nginx:latest
```

**🔴 Тест:**
```go
func TestParseFile_Minimal(t *testing.T) {
    cf, err := compose.ParseFile("testdata/minimal.yml")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if cf.Version != "3.8" {
        t.Errorf("expected version 3.8, got %q", cf.Version)
    }
    svc, ok := cf.Services["web"]
    if !ok {
        t.Fatal("service 'web' not found")
    }
    if svc.Image != "nginx:latest" {
        t.Errorf("expected image nginx:latest, got %q", svc.Image)
    }
}
```

**🟢 Реализация — расширить `types.go`:**
```go
// internal/compose/types.go
package compose

type ComposeFile struct {
    Version  string              `yaml:"version"`
    Services map[string]Service  `yaml:"services"`
    Networks map[string]Network  `yaml:"networks"`
    Volumes  map[string]Volume   `yaml:"volumes"`
    Secrets  map[string]Secret   `yaml:"secrets"`
    Configs  map[string]Config   `yaml:"configs"`
}

type Service struct {
    Image       string      `yaml:"image"`
    Command     interface{} `yaml:"command"`      // string или []string
    Entrypoint  interface{} `yaml:"entrypoint"`
    Environment interface{} `yaml:"environment"`  // []string или map[string]string
    Ports       []interface{} `yaml:"ports"`
    Volumes     []interface{} `yaml:"volumes"`
    Networks    interface{} `yaml:"networks"`     // []string или map
    Labels      interface{} `yaml:"labels"`
    Deploy      DeployConfig       `yaml:"deploy"`
    HealthCheck *HealthCheckConfig `yaml:"healthcheck"`
    ExtraHosts  []string           `yaml:"extra_hosts"`
    DNS         []string           `yaml:"dns"`
    DependsOn   interface{}        `yaml:"depends_on"`
    Logging     *LoggingConfig     `yaml:"logging"`
    // Capabilities
    CapAdd  []string `yaml:"cap_add"`
    CapDrop []string `yaml:"cap_drop"`
    // User
    User       string `yaml:"user"`
    WorkingDir string `yaml:"working_dir"`
    Hostname   string `yaml:"hostname"`
    // Secrets & Configs
    Secrets []interface{} `yaml:"secrets"`
    Configs []interface{} `yaml:"configs"`
}

type DeployConfig struct {
    Mode          string         `yaml:"mode"`
    Replicas      *uint64        `yaml:"replicas"`
    UpdateConfig  *UpdateConfig  `yaml:"update_config"`
    RollbackConfig *RollbackConfig `yaml:"rollback_config"`
    RestartPolicy *RestartPolicy `yaml:"restart_policy"`
    Resources     Resources      `yaml:"resources"`
    Placement     Placement      `yaml:"placement"`
    Labels        interface{}    `yaml:"labels"`
    EndpointMode  string         `yaml:"endpoint_mode"`
}

type UpdateConfig struct {
    Parallelism     *uint64 `yaml:"parallelism"`
    Delay           string  `yaml:"delay"`
    FailureAction   string  `yaml:"failure_action"`
    Monitor         string  `yaml:"monitor"`
    MaxFailureRatio float64 `yaml:"max_failure_ratio"`
    Order           string  `yaml:"order"`
}

type RollbackConfig struct {
    Parallelism     *uint64 `yaml:"parallelism"`
    Delay           string  `yaml:"delay"`
    FailureAction   string  `yaml:"failure_action"`
    Monitor         string  `yaml:"monitor"`
    MaxFailureRatio float64 `yaml:"max_failure_ratio"`
    Order           string  `yaml:"order"`
}

type RestartPolicy struct {
    Condition   string `yaml:"condition"`
    Delay       string `yaml:"delay"`
    MaxAttempts *uint64 `yaml:"max_attempts"`
    Window      string `yaml:"window"`
}

type Resources struct {
    Limits       ResourceSpec `yaml:"limits"`
    Reservations ResourceSpec `yaml:"reservations"`
}

type ResourceSpec struct {
    CPUs   string `yaml:"cpus"`
    Memory string `yaml:"memory"`
}

type Placement struct {
    Constraints []string          `yaml:"constraints"`
    Preferences []PlacementPref   `yaml:"preferences"`
}

type PlacementPref struct {
    Spread string `yaml:"spread"`
}

type HealthCheckConfig struct {
    Test        []string `yaml:"test"`
    Interval    string   `yaml:"interval"`
    Timeout     string   `yaml:"timeout"`
    Retries     *int     `yaml:"retries"`
    StartPeriod string   `yaml:"start_period"`
    Disable     bool     `yaml:"disable"`
}

type LoggingConfig struct {
    Driver  string            `yaml:"driver"`
    Options map[string]string `yaml:"options"`
}

type Network struct {
    Driver     string            `yaml:"driver"`
    External   interface{}       `yaml:"external"` // bool или {name: string}
    Attachable bool              `yaml:"attachable"`
    IPAM       *IPAMConfig       `yaml:"ipam"`
    Labels     interface{}       `yaml:"labels"`
    DriverOpts map[string]string `yaml:"driver_opts"`
}

type IPAMConfig struct {
    Driver string       `yaml:"driver"`
    Config []IPAMPool   `yaml:"config"`
}

type IPAMPool struct {
    Subnet string `yaml:"subnet"`
}

type Volume struct {
    Driver     string            `yaml:"driver"`
    External   interface{}       `yaml:"external"`
    Labels     interface{}       `yaml:"labels"`
    DriverOpts map[string]string `yaml:"driver_opts"`
}

type Secret struct {
    File     string      `yaml:"file"`
    External interface{} `yaml:"external"`
    Labels   interface{} `yaml:"labels"`
}

type Config struct {
    File     string      `yaml:"file"`
    External interface{} `yaml:"external"`
    Labels   interface{} `yaml:"labels"`
}
```

**Реализация `parser.go`:**
```go
// internal/compose/parser.go
package compose

import (
    "fmt"
    "os"
    "gopkg.in/yaml.v3"
)

func ParseFile(path string) (*ComposeFile, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read compose file: %w", err)
    }
    return ParseBytes(data)
}

func ParseBytes(data []byte) (*ComposeFile, error) {
    var cf ComposeFile
    if err := yaml.Unmarshal(data, &cf); err != nil {
        return nil, fmt.Errorf("parse compose file: %w", err)
    }
    return &cf, nil
}
```

---

### Цикл 3: Парсер — полный сервис

**Создаём testdata:**
```yaml
# internal/compose/testdata/full-service.yml
version: "3.8"
services:
  app:
    image: myapp:1.0
    deploy:
      replicas: 3
      update_config:
        parallelism: 1
        delay: 10s
        failure_action: rollback
        order: start-first
      restart_policy:
        condition: on-failure
        delay: 5s
        max_attempts: 3
      resources:
        limits:
          cpus: "0.5"
          memory: 128M
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 60s
    environment:
      - APP_ENV=production
      - DB_HOST=db
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

**🔴 Тест:**
```go
func TestParseFile_FullService(t *testing.T) {
    cf, err := compose.ParseFile("testdata/full-service.yml")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    svc := cf.Services["app"]

    if svc.Deploy.Replicas == nil || *svc.Deploy.Replicas != 3 {
        t.Errorf("expected replicas=3")
    }
    if svc.HealthCheck == nil {
        t.Fatal("expected healthcheck")
    }
    if svc.HealthCheck.Interval != "30s" {
        t.Errorf("expected interval=30s, got %q", svc.HealthCheck.Interval)
    }
    if len(svc.ExtraHosts) != 1 {
        t.Errorf("expected 1 extra host")
    }
}
```

Тест должен пройти с уже написанными структурами.

---

### Цикл 4: Конвертер — сервис → SwarmSpec (реплики)

**🔴 Тест:**
```go
// internal/compose/converter_test.go
package compose_test

import (
    "testing"
    "github.com/SomeBlackMagic/stackman/internal/compose"
)

func TestConvertService_Replicas(t *testing.T) {
    replicas := uint64(3)
    svc := compose.Service{
        Image: "nginx:latest",
        Deploy: compose.DeployConfig{
            Replicas: &replicas,
        },
    }
    spec, err := compose.ConvertToSwarmSpec("web", svc, "mystack")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if spec.Mode.Replicated == nil {
        t.Fatal("expected replicated mode")
    }
    if *spec.Mode.Replicated.Replicas != 3 {
        t.Errorf("expected 3 replicas, got %d", *spec.Mode.Replicated.Replicas)
    }
    // Имя сервиса должно быть с префиксом стека
    if spec.Name != "mystack_web" {
        t.Errorf("expected name mystack_web, got %q", spec.Name)
    }
}
```

**🟢 Реализация — начало `converter.go`:**
```go
// internal/compose/converter.go
package compose

import (
    "fmt"
    "github.com/docker/docker/api/types/swarm"
)

func ConvertToSwarmSpec(serviceName string, svc Service, stackName string) (swarm.ServiceSpec, error) {
    spec := swarm.ServiceSpec{
        Annotations: swarm.Annotations{
            Name: fmt.Sprintf("%s_%s", stackName, serviceName),
            Labels: map[string]string{
                "com.docker.stack.namespace": stackName,
                "com.docker.stack.image":     svc.Image,
            },
        },
    }

    // Режим деплоя
    if svc.Deploy.Mode == "global" {
        spec.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}
    } else {
        replicas := uint64(1)
        if svc.Deploy.Replicas != nil {
            replicas = *svc.Deploy.Replicas
        }
        spec.Mode = swarm.ServiceMode{
            Replicated: &swarm.ReplicatedService{Replicas: &replicas},
        }
    }

    // TaskTemplate
    spec.TaskTemplate = swarm.TaskSpec{
        ContainerSpec: &swarm.ContainerSpec{
            Image: svc.Image,
        },
    }

    return spec, nil
}
```

---

### Цикл 5: Конвертер — healthcheck

**🔴 Тест:**
```go
func TestConvertService_Healthcheck(t *testing.T) {
    retries := 3
    svc := compose.Service{
        Image: "nginx:latest",
        HealthCheck: &compose.HealthCheckConfig{
            Test:     []string{"CMD", "curl", "-f", "http://localhost/health"},
            Interval: "30s",
            Timeout:  "10s",
            Retries:  &retries,
        },
    }
    spec, err := compose.ConvertToSwarmSpec("web", svc, "stack")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    hc := spec.TaskTemplate.ContainerSpec.Healthcheck
    if hc == nil {
        t.Fatal("expected healthcheck in spec")
    }
    if hc.Interval.String() != "30s" {
        t.Errorf("interval mismatch")
    }
}

func TestConvertService_HealthcheckDisabled(t *testing.T) {
    svc := compose.Service{
        Image:       "nginx:latest",
        HealthCheck: &compose.HealthCheckConfig{Disable: true},
    }
    spec, err := compose.ConvertToSwarmSpec("web", svc, "stack")
    if err != nil {
        t.Fatal(err)
    }
    hc := spec.TaskTemplate.ContainerSpec.Healthcheck
    if hc == nil {
        t.Fatal("expected healthcheck struct (with Test=[NONE])")
    }
    if len(hc.Test) == 0 || hc.Test[0] != "NONE" {
        t.Errorf("expected NONE test, got %v", hc.Test)
    }
}
```

**🟢 Добавить в `converter.go`:**
```go
// в ConvertToSwarmSpec после TaskTemplate:
if svc.HealthCheck != nil {
    spec.TaskTemplate.ContainerSpec.Healthcheck = convertHealthCheck(svc.HealthCheck)
}
```

```go
func convertHealthCheck(hc *HealthCheckConfig) *container.HealthConfig {
    if hc.Disable {
        return &container.HealthConfig{Test: []string{"NONE"}}
    }
    h := &container.HealthConfig{Test: hc.Test}
    if hc.Interval != "" {
        h.Interval, _ = time.ParseDuration(hc.Interval)
    }
    if hc.Timeout != "" {
        h.Timeout, _ = time.ParseDuration(hc.Timeout)
    }
    if hc.Retries != nil {
        h.Retries = *hc.Retries
    }
    if hc.StartPeriod != "" {
        h.StartPeriod, _ = time.ParseDuration(hc.StartPeriod)
    }
    return h
}
```

---

### Цикл 6: Extra hosts — замена `host-gateway`

**🔴 Тест:**
```go
// internal/compose/extra_hosts_test.go
package compose_test

import (
    "testing"
    "github.com/SomeBlackMagic/stackman/internal/compose"
)

func TestReplaceHostGatewayToken(t *testing.T) {
    tests := []struct {
        input   []string
        gateway string
        want    []string
    }{
        {
            input:   []string{"host.docker.internal:host-gateway"},
            gateway: "172.17.0.1",
            want:    []string{"host.docker.internal:172.17.0.1"},
        },
        {
            input:   []string{"db:192.168.1.1"},
            gateway: "172.17.0.1",
            want:    []string{"db:192.168.1.1"}, // без изменений
        },
        {
            input:   []string{},
            gateway: "172.17.0.1",
            want:    []string{},
        },
    }
    for _, tt := range tests {
        got := compose.ReplaceHostGatewayToken(tt.input, tt.gateway)
        if !reflect.DeepEqual(got, tt.want) {
            t.Errorf("input=%v gateway=%v: got %v, want %v", tt.input, tt.gateway, got, tt.want)
        }
    }
}

func TestHasHostGateway(t *testing.T) {
    if !compose.HasHostGateway([]string{"host.docker.internal:host-gateway"}) {
        t.Error("expected true")
    }
    if compose.HasHostGateway([]string{"db:192.168.1.1"}) {
        t.Error("expected false")
    }
}
```

**🟢 Реализация:**
```go
// internal/compose/extra_hosts.go
package compose

import "strings"

const hostGatewayToken = "host-gateway"

func HasHostGateway(hosts []string) bool {
    for _, h := range hosts {
        if strings.HasSuffix(h, ":"+hostGatewayToken) {
            return true
        }
    }
    return false
}

func ReplaceHostGatewayToken(hosts []string, gatewayIP string) []string {
    result := make([]string, len(hosts))
    for i, h := range hosts {
        if strings.HasSuffix(h, ":"+hostGatewayToken) {
            result[i] = strings.TrimSuffix(h, ":"+hostGatewayToken) + ":" + gatewayIP
        } else {
            result[i] = h
        }
    }
    return result
}
```

---

### Цикл 7: Конвертер сетей и томов

**🔴 Тесты:**
```go
func TestConvertNetwork(t *testing.T) {
    net := compose.Network{Driver: "overlay", Attachable: true}
    opts, err := compose.ConvertNetwork("mynet", net, "mystack")
    if err != nil {
        t.Fatal(err)
    }
    if opts.Driver != "overlay" {
        t.Errorf("expected overlay driver")
    }
    wantLabel := "mystack"
    if opts.Labels["com.docker.stack.namespace"] != wantLabel {
        t.Errorf("missing stack label")
    }
}

func TestConvertVolume(t *testing.T) {
    vol := compose.Volume{Driver: "local"}
    opts, err := compose.ConvertVolume("data", vol, "mystack")
    if err != nil {
        t.Fatal(err)
    }
    if opts.Driver != "local" {
        t.Errorf("expected local driver")
    }
}
```

**🟢 Добавить в `converter.go`:**
```go
func ConvertNetwork(name string, net Network, stackName string) (network.CreateOptions, error) {
    driver := net.Driver
    if driver == "" {
        driver = "overlay"
    }
    return network.CreateOptions{
        Driver:     driver,
        Attachable: net.Attachable,
        Labels: map[string]string{
            "com.docker.stack.namespace": stackName,
        },
    }, nil
}

func ConvertVolume(name string, vol Volume, stackName string) (volume.CreateOptions, error) {
    driver := vol.Driver
    if driver == "" {
        driver = "local"
    }
    return volume.CreateOptions{
        Driver: driver,
        Labels: map[string]string{
            "com.docker.stack.namespace": stackName,
        },
        DriverOpts: vol.DriverOpts,
    }, nil
}
```

---

## Критерии завершения этапа

- [ ] `TestParseFile_NotFound` — ошибка для несуществующего файла
- [ ] `TestParseFile_Minimal` — корректный парсинг версии и образа
- [ ] `TestParseFile_FullService` — replicas, healthcheck, extra_hosts
- [ ] `TestConvertService_Replicas` — spec.Mode.Replicated.Replicas == 3, Name == "stack_web"
- [ ] `TestConvertService_Healthcheck` — healthcheck в spec
- [ ] `TestConvertService_HealthcheckDisabled` — Test[0] == "NONE"
- [ ] `TestReplaceHostGatewayToken` — замена host-gateway токена
- [ ] `TestConvertNetwork` / `TestConvertVolume` — корректные опции создания
- [ ] `go test ./internal/compose/...` — все тесты зелёные

## Следующий этап
→ [Этап 3: Deployment ID](./03-deployment-id.md)
