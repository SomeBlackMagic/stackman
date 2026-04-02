package compose

import (
	"reflect"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

func TestConvertComposeToSwarmSpecs_Errors(t *testing.T) {
	_, err := ConvertComposeToSwarmSpecs(nil, "stack")
	if err == nil {
		t.Fatal("expected error for nil compose file")
	}

	_, err = ConvertComposeToSwarmSpecs(&ComposeFile{
		Services: map[string]*Service{"bad": nil},
	}, "stack")
	if err == nil {
		t.Fatal("expected error for nil service")
	}
}

func TestConvertEnvironment_Table(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    []string
		wantErr bool
	}{
		{
			name:  "slice string",
			input: []string{"A=1", "B=2"},
			want:  []string{"A=1", "B=2"},
		},
		{
			name:  "slice interface",
			input: []interface{}{"A=1", "B=2"},
			want:  []string{"A=1", "B=2"},
		},
		{
			name:  "map string string sorted",
			input: map[string]string{"B": "2", "A": "1"},
			want:  []string{"A=1", "B=2"},
		},
		{
			name:  "map string interface sorted",
			input: map[string]interface{}{"B": 2, "A": "1"},
			want:  []string{"A=1", "B=2"},
		},
		{
			name:    "invalid list item type",
			input:   []interface{}{"A=1", 2},
			wantErr: true,
		},
		{
			name:    "invalid type",
			input:   42,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertEnvironment(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertCommand_Table(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    []string
		wantErr bool
	}{
		{
			name:  "string command",
			input: "echo hello",
			want:  []string{"echo", "hello"},
		},
		{
			name:  "slice string",
			input: []string{"echo", "hello"},
			want:  []string{"echo", "hello"},
		},
		{
			name:  "slice interface",
			input: []interface{}{"echo", "hello"},
			want:  []string{"echo", "hello"},
		},
		{
			name:    "invalid list item",
			input:   []interface{}{"echo", 1},
			wantErr: true,
		},
		{
			name:    "invalid type",
			input:   true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertCommand(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertToStringSlice_Table(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    []string
		wantErr bool
	}{
		{
			name:  "single string",
			input: "8.8.8.8",
			want:  []string{"8.8.8.8"},
		},
		{
			name:  "slice string",
			input: []string{"8.8.8.8", "1.1.1.1"},
			want:  []string{"8.8.8.8", "1.1.1.1"},
		},
		{
			name:  "slice interface",
			input: []interface{}{"8.8.8.8", "1.1.1.1"},
			want:  []string{"8.8.8.8", "1.1.1.1"},
		},
		{
			name:    "invalid list item",
			input:   []interface{}{"8.8.8.8", 1},
			wantErr: true,
		},
		{
			name:    "invalid type",
			input:   123,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertToStringSlice(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInterfaceToUint32_Table(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    uint32
		wantErr bool
	}{
		{name: "int", input: int(12), want: 12},
		{name: "int64", input: int64(13), want: 13},
		{name: "uint64", input: uint64(14), want: 14},
		{name: "float64", input: float64(15), want: 15},
		{name: "string", input: "16", want: 16},
		{name: "invalid string", input: "abc", wantErr: true},
		{name: "invalid type", input: struct{}{}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := interfaceToUint32(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParsePortMap_Table(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]interface{}
		want    uint32
		wantErr bool
	}{
		{
			name: "valid map defaults",
			input: map[string]interface{}{
				"target": 80,
			},
			want: 80,
		},
		{
			name: "valid map full",
			input: map[string]interface{}{
				"target":    "80",
				"published": 8080,
				"protocol":  "udp",
				"mode":      "host",
			},
			want: 8080,
		},
		{
			name: "missing target",
			input: map[string]interface{}{
				"published": 8080,
			},
			wantErr: true,
		},
		{
			name: "bad target type",
			input: map[string]interface{}{
				"target": true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePortMap(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.PublishedPort != tt.want {
				t.Fatalf("published port got %d, want %d", got.PublishedPort, tt.want)
			}
		})
	}
}

func TestParsePortString_Error(t *testing.T) {
	_, err := parsePortString("bad:80")
	if err == nil {
		t.Fatal("expected error for invalid published port")
	}

	_, err = parsePortString("80:bad")
	if err == nil {
		t.Fatal("expected error for invalid target port")
	}

	_, err = parsePortString("1:2:3")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestConvertDeploy_UnsupportedMode(t *testing.T) {
	spec := &swarm.ServiceSpec{}
	err := convertDeploy(spec, &DeployConfig{Mode: "job"})
	if err == nil {
		t.Fatal("expected unsupported mode error")
	}
}

func TestConvertPorts_Table(t *testing.T) {
	ports, err := convertPorts([]interface{}{
		"8080:80",
		map[string]interface{}{
			"target":    53,
			"published": 5353,
			"protocol":  "udp",
			"mode":      "host",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(ports))
	}

	if ports[1].TargetPort != 53 || ports[1].PublishedPort != 5353 {
		t.Fatalf("unexpected long syntax port config: %+v", ports[1])
	}

	_, err = convertPorts([]interface{}{true})
	if err == nil {
		t.Fatal("expected error for unsupported port type")
	}
}

func TestConvertHealthCheck_StringAndErrors(t *testing.T) {
	hc, err := convertHealthCheck(&HealthCheck{
		Test:        "curl -f http://localhost/health",
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     2,
		StartPeriod: "5s",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(hc.Test, []string{"CMD-SHELL", "curl -f http://localhost/health"}) {
		t.Fatalf("unexpected test command: %v", hc.Test)
	}

	_, err = convertHealthCheck(&HealthCheck{Interval: "bad"})
	if err == nil {
		t.Fatal("expected interval parse error")
	}
	_, err = convertHealthCheck(&HealthCheck{Timeout: "bad"})
	if err == nil {
		t.Fatal("expected timeout parse error")
	}
	_, err = convertHealthCheck(&HealthCheck{StartPeriod: "bad"})
	if err == nil {
		t.Fatal("expected start_period parse error")
	}
}

func TestConvertDeploy_TableErrors(t *testing.T) {
	tests := []struct {
		name   string
		deploy *DeployConfig
	}{
		{
			name: "bad update delay",
			deploy: &DeployConfig{
				UpdateConfig: &UpdateConfig{Delay: "bad"},
			},
		},
		{
			name: "bad update monitor",
			deploy: &DeployConfig{
				UpdateConfig: &UpdateConfig{Monitor: "bad"},
			},
		},
		{
			name: "bad restart delay",
			deploy: &DeployConfig{
				RestartPolicy: &RestartPolicy{Delay: "bad"},
			},
		},
		{
			name: "bad restart window",
			deploy: &DeployConfig{
				RestartPolicy: &RestartPolicy{Window: "bad"},
			},
		},
		{
			name: "bad cpu limit",
			deploy: &DeployConfig{
				Resources: &Resources{Limits: &ResourceLimit{CPUs: "bad"}},
			},
		},
		{
			name: "bad memory limit",
			deploy: &DeployConfig{
				Resources: &Resources{Limits: &ResourceLimit{Memory: "bad"}},
			},
		},
		{
			name: "bad cpu reservation",
			deploy: &DeployConfig{
				Resources: &Resources{Reservations: &ResourceLimit{CPUs: "bad"}},
			},
		},
		{
			name: "bad memory reservation",
			deploy: &DeployConfig{
				Resources: &Resources{Reservations: &ResourceLimit{Memory: "bad"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &swarm.ServiceSpec{}
			err := convertDeploy(spec, tt.deploy)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestConvertDeploy_GlobalAndPlacement(t *testing.T) {
	spec := &swarm.ServiceSpec{}
	err := convertDeploy(spec, &DeployConfig{
		Mode: "global",
		RollbackConfig: &UpdateConfig{
			Parallelism: 1,
			Delay:       "1s",
		},
		Placement: &Placement{
			Constraints: []string{"node.role==manager"},
			Preferences: []PlacementPreference{{Spread: "node.labels.zone"}},
			MaxReplicas: 2,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Mode.Global == nil {
		t.Fatal("expected global mode")
	}
	if spec.RollbackConfig == nil || spec.RollbackConfig.Delay != time.Second {
		t.Fatalf("unexpected rollback config: %+v", spec.RollbackConfig)
	}
	if spec.TaskTemplate.Placement == nil || len(spec.TaskTemplate.Placement.Preferences) != 1 {
		t.Fatalf("unexpected placement: %+v", spec.TaskTemplate.Placement)
	}
}

func TestConvertToSwarmSpec_ErrorBranches(t *testing.T) {
	tests := []struct {
		name string
		svc  Service
	}{
		{
			name: "bad stop grace period",
			svc:  Service{Image: "x", StopGracePeriod: "bad"},
		},
		{
			name: "bad dns",
			svc:  Service{Image: "x", DNS: 123},
		},
		{
			name: "bad dns search",
			svc:  Service{Image: "x", DNSSearch: 123},
		},
		{
			name: "bad environment",
			svc:  Service{Image: "x", Environment: 123},
		},
		{
			name: "bad entrypoint",
			svc:  Service{Image: "x", Entrypoint: 123},
		},
		{
			name: "bad command",
			svc:  Service{Image: "x", Command: 123},
		},
		{
			name: "bad deploy",
			svc:  Service{Image: "x", Deploy: &DeployConfig{Mode: "job"}},
		},
		{
			name: "bad ports",
			svc:  Service{Image: "x", Ports: []interface{}{"bad:80"}},
		},
		{
			name: "bad healthcheck",
			svc:  Service{Image: "x", HealthCheck: &HealthCheck{Interval: "bad"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ConvertToSwarmSpec("svc", tt.svc, "stack")
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestConvertNetworkAndVolume_Defaults(t *testing.T) {
	netOpts, err := ConvertNetwork("n", Network{}, "stack")
	if err != nil {
		t.Fatalf("unexpected network error: %v", err)
	}
	if netOpts.Driver != "overlay" {
		t.Fatalf("expected default network driver overlay, got %q", netOpts.Driver)
	}

	volOpts, err := ConvertVolume("v", Volume{}, "stack")
	if err != nil {
		t.Fatalf("unexpected volume error: %v", err)
	}
	if volOpts.Driver != "local" {
		t.Fatalf("expected default volume driver local, got %q", volOpts.Driver)
	}
}
