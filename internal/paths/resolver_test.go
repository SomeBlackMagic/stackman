package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolver_Resolve(t *testing.T) {
	// Create a resolver with a known base path
	r := &Resolver{
		basePath: "/project",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "absolute path",
			input:    "/var/data",
			expected: "/var/data",
		},
		{
			name:     "current directory relative",
			input:    "./data",
			expected: "/project/data",
		},
		{
			name:     "parent directory relative",
			input:    "../data",
			expected: "/project/../data",
		},
		{
			name:     "simple relative",
			input:    "data",
			expected: "/project/data",
		},
		{
			name:     "nfs protocol",
			input:    "nfs://server/share",
			expected: "nfs://server/share",
		},
		{
			name:     "nested relative",
			input:    "data/subfolder",
			expected: "/project/data/subfolder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Resolve(tt.input)
			// Clean paths for comparison
			result = filepath.Clean(result)
			expected := filepath.Clean(tt.expected)
			if result != expected {
				t.Errorf("Resolve(%q) = %q, want %q", tt.input, result, expected)
			}
		})
	}
}

func TestNewResolver(t *testing.T) {
	// Test with SWARM_STACK_PATH set
	testPath := "/test/path"
	os.Setenv("SWARM_STACK_PATH", testPath)
	defer os.Unsetenv("SWARM_STACK_PATH")

	r, err := NewResolver()
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	absTestPath, _ := filepath.Abs(testPath)
	if r.BasePath() != absTestPath {
		t.Errorf("NewResolver() basePath = %q, want %q", r.BasePath(), absTestPath)
	}

	// Test without SWARM_STACK_PATH (should use current directory)
	os.Unsetenv("SWARM_STACK_PATH")
	r2, err := NewResolver()
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	cwd, _ := os.Getwd()
	if r2.BasePath() != cwd {
		t.Errorf("NewResolver() basePath = %q, want %q", r2.BasePath(), cwd)
	}
}
