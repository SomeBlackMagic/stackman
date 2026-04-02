package deployment_test

import (
	"regexp"
	"testing"

	"github.com/SomeBlackMagic/stackman/internal/deployment"
)

var deployIDPattern = regexp.MustCompile(`^deploy-\d{8}-\d{6}-[0-9a-f]{8}$`)

func TestGenerateDeployID_Format(t *testing.T) {
	id := deployment.GenerateDeployID()
	if !deployIDPattern.MatchString(id) {
		t.Errorf("invalid deploy ID format: %q\nexpected: deploy-YYYYMMDD-HHMMSS-RANDOM8HEX", id)
	}
}

func TestGenerateDeployID_Unique(t *testing.T) {
	seen := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id := deployment.GenerateDeployID()
		if seen[id] {
			t.Fatalf("duplicate deploy ID: %q", id)
		}
		seen[id] = true
	}
}

func TestDeployIDLabel(t *testing.T) {
	want := "com.stackman.deploy.id"
	if deployment.DeployIDLabel != want {
		t.Errorf("expected label %q, got %q", want, deployment.DeployIDLabel)
	}
}
