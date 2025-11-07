package deployment

import (
	"regexp"
	"testing"
)

func TestGenerateDeployID(t *testing.T) {
	// Generate multiple IDs to ensure uniqueness
	id1 := GenerateDeployID()
	id2 := GenerateDeployID()

	// Test format: deploy-YYYYMMDD-HHMMSS-RANDOM
	// Example: deploy-20250107-143052-a1b2c3d4
	pattern := `^deploy-\d{8}-\d{6}-[0-9a-f]{8}$`
	re := regexp.MustCompile(pattern)

	if !re.MatchString(id1) {
		t.Errorf("DeployID format is invalid: %s (expected pattern: %s)", id1, pattern)
	}

	if !re.MatchString(id2) {
		t.Errorf("DeployID format is invalid: %s (expected pattern: %s)", id2, pattern)
	}

	// Test that IDs are different (random part should differ)
	if id1 == id2 {
		t.Errorf("Expected unique IDs, but got identical: %s", id1)
	}

	// Test that IDs start with "deploy-"
	if id1[:7] != "deploy-" {
		t.Errorf("DeployID should start with 'deploy-', got: %s", id1)
	}

	// Test length (deploy-YYYYMMDD-HHMMSS-RANDOM = 7+8+1+6+1+8 = 31 chars)
	expectedLength := 31
	if len(id1) != expectedLength {
		t.Errorf("DeployID length should be %d, got: %d (%s)", expectedLength, len(id1), id1)
	}
}

func TestDeployIDLabel(t *testing.T) {
	expectedLabel := "com.stackman.deploy.id"
	if DeployIDLabel != expectedLabel {
		t.Errorf("DeployIDLabel should be %s, got: %s", expectedLabel, DeployIDLabel)
	}
}

func BenchmarkGenerateDeployID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateDeployID()
	}
}
