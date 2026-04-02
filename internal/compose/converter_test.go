package compose_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/SomeBlackMagic/stackman/internal/compose"
)

func TestConvertService_Replicas(t *testing.T) {
	replicas := 3
	svc := compose.Service{
		Image: "nginx:latest",
		Deploy: &compose.DeployConfig{
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
	if spec.Mode.Replicated.Replicas == nil || *spec.Mode.Replicated.Replicas != 3 {
		t.Fatalf("expected 3 replicas")
	}
	if spec.Name != "mystack_web" {
		t.Fatalf("expected name mystack_web, got %q", spec.Name)
	}
}

func TestConvertComposeToSwarmSpecs_FromParsedFile(t *testing.T) {
	cf, err := compose.ParseFile("testdata/minimal.yml")
	if err != nil {
		t.Fatalf("parse file: %v", err)
	}

	specs, err := compose.ConvertComposeToSwarmSpecs(cf, "mystack")
	if err != nil {
		t.Fatalf("convert compose: %v", err)
	}

	spec, ok := specs["web"]
	if !ok {
		t.Fatal("expected converted spec for service 'web'")
	}

	if spec.Name != "mystack_web" {
		t.Fatalf("expected name mystack_web, got %q", spec.Name)
	}

	if spec.TaskTemplate.ContainerSpec == nil || spec.TaskTemplate.ContainerSpec.Image != "nginx:latest" {
		t.Fatalf("expected image nginx:latest")
	}
}

func TestConvertService_Healthcheck(t *testing.T) {
	svc := compose.Service{
		Image: "nginx:latest",
		HealthCheck: &compose.HealthCheck{
			Test:     []string{"CMD", "curl", "-f", "http://localhost/health"},
			Interval: "30s",
			Timeout:  "10s",
			Retries:  3,
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
		t.Fatalf("expected interval 30s, got %s", hc.Interval.String())
	}
}

func TestConvertService_HealthcheckDisabled(t *testing.T) {
	svc := compose.Service{
		Image:       "nginx:latest",
		HealthCheck: &compose.HealthCheck{Disable: true},
	}

	spec, err := compose.ConvertToSwarmSpec("web", svc, "stack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hc := spec.TaskTemplate.ContainerSpec.Healthcheck
	if hc == nil {
		t.Fatal("expected healthcheck struct (with Test=[NONE])")
	}
	if len(hc.Test) == 0 || hc.Test[0] != "NONE" {
		t.Fatalf("expected NONE test, got %v", hc.Test)
	}
}

func TestConvertNetwork(t *testing.T) {
	netCfg := compose.Network{Driver: "overlay", Attachable: true}

	opts, err := compose.ConvertNetwork("mynet", netCfg, "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opts.Driver != "overlay" {
		t.Fatalf("expected overlay driver, got %q", opts.Driver)
	}

	if opts.Labels["com.docker.stack.namespace"] != "mystack" {
		t.Fatal("missing stack label")
	}
}

func TestConvertVolume(t *testing.T) {
	vol := compose.Volume{Driver: "local"}

	opts, err := compose.ConvertVolume("data", vol, "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opts.Driver != "local" {
		t.Fatalf("expected local driver, got %q", opts.Driver)
	}
}

func TestConvertService_EnvironmentMap(t *testing.T) {
	svc := compose.Service{
		Image:       "nginx:latest",
		Environment: map[string]interface{}{"B": "2", "A": "1"},
	}

	spec, err := compose.ConvertToSwarmSpec("web", svc, "stack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"A=1", "B=2"}
	if !reflect.DeepEqual(spec.TaskTemplate.ContainerSpec.Env, want) {
		t.Fatalf("unexpected env: got %v, want %v", spec.TaskTemplate.ContainerSpec.Env, want)
	}
}

func TestConvertService_CommandAndEntrypoint(t *testing.T) {
	svc := compose.Service{
		Image:      "alpine:latest",
		Command:    []interface{}{"-c", "echo hi"},
		Entrypoint: []interface{}{"/bin/sh"},
	}

	spec, err := compose.ConvertToSwarmSpec("job", svc, "stack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(spec.TaskTemplate.ContainerSpec.Command, []string{"/bin/sh"}) {
		t.Fatalf("unexpected command(entrypoint): %v", spec.TaskTemplate.ContainerSpec.Command)
	}
	if !reflect.DeepEqual(spec.TaskTemplate.ContainerSpec.Args, []string{"-c", "echo hi"}) {
		t.Fatalf("unexpected args(command): %v", spec.TaskTemplate.ContainerSpec.Args)
	}
}

func TestConvertService_PortsShortSyntax(t *testing.T) {
	svc := compose.Service{
		Image: "nginx:latest",
		Ports: []interface{}{"8080:80", "5353:53/udp"},
	}

	spec, err := compose.ConvertToSwarmSpec("web", svc, "stack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.EndpointSpec == nil {
		t.Fatal("expected endpoint spec")
	}
	if len(spec.EndpointSpec.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(spec.EndpointSpec.Ports))
	}

	first := spec.EndpointSpec.Ports[0]
	if first.PublishedPort != 8080 || first.TargetPort != 80 || string(first.Protocol) != "tcp" {
		t.Fatalf("unexpected first port: %+v", first)
	}

	second := spec.EndpointSpec.Ports[1]
	if second.PublishedPort != 5353 || second.TargetPort != 53 || string(second.Protocol) != "udp" {
		t.Fatalf("unexpected second port: %+v", second)
	}
}

func TestConvertService_DeployAdvanced(t *testing.T) {
	replicas := 2
	maxAttempts := 3
	svc := compose.Service{
		Image: "myapp:1.0",
		Deploy: &compose.DeployConfig{
			Replicas: &replicas,
			UpdateConfig: &compose.UpdateConfig{
				Parallelism:     1,
				Delay:           "10s",
				FailureAction:   "rollback",
				Monitor:         "30s",
				MaxFailureRatio: 0.25,
				Order:           "start-first",
			},
			RestartPolicy: &compose.RestartPolicy{
				Condition:   "on-failure",
				Delay:       "5s",
				MaxAttempts: &maxAttempts,
				Window:      "1m",
			},
			Resources: &compose.Resources{
				Limits: &compose.ResourceLimit{
					CPUs:   "0.5",
					Memory: "128M",
				},
				Reservations: &compose.ResourceLimit{
					CPUs:   "0.25",
					Memory: "64M",
				},
			},
		},
	}

	spec, err := compose.ConvertToSwarmSpec("app", svc, "stack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.UpdateConfig == nil {
		t.Fatal("expected update config")
	}
	if spec.UpdateConfig.Parallelism != 1 || spec.UpdateConfig.Delay != 10*time.Second {
		t.Fatalf("unexpected update config: %+v", spec.UpdateConfig)
	}
	if spec.TaskTemplate.RestartPolicy == nil {
		t.Fatal("expected restart policy")
	}
	if spec.TaskTemplate.RestartPolicy.Delay == nil || *spec.TaskTemplate.RestartPolicy.Delay != 5*time.Second {
		t.Fatalf("unexpected restart delay: %+v", spec.TaskTemplate.RestartPolicy)
	}
	if spec.TaskTemplate.Resources == nil || spec.TaskTemplate.Resources.Limits == nil || spec.TaskTemplate.Resources.Reservations == nil {
		t.Fatal("expected resources limits and reservations")
	}
	if spec.TaskTemplate.Resources.Limits.NanoCPUs != 500000000 {
		t.Fatalf("unexpected cpu limit: %d", spec.TaskTemplate.Resources.Limits.NanoCPUs)
	}
	if spec.TaskTemplate.Resources.Reservations.NanoCPUs != 250000000 {
		t.Fatalf("unexpected cpu reservation: %d", spec.TaskTemplate.Resources.Reservations.NanoCPUs)
	}
}
