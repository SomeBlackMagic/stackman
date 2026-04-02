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

func TestParseFile_FullService(t *testing.T) {
	cf, err := compose.ParseFile("testdata/full-service.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cf.Services == nil {
		t.Fatal("services map is nil")
	}
	svc, ok := cf.Services["app"]
	if !ok {
		t.Fatal("service 'app' not found")
	}

	if svc.Deploy == nil || svc.Deploy.Replicas == nil || *svc.Deploy.Replicas != 3 {
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

func TestParseBytes_InvalidYAML(t *testing.T) {
	_, err := compose.ParseBytes([]byte("services:\n  web:\n    image: nginx\n    invalid: [[["))
	if err == nil {
		t.Fatal("expected parse error for invalid YAML")
	}
}
