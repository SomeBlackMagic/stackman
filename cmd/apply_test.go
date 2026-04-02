package cmd

import (
	"strings"
	"testing"
)

func TestApplyRequiredFlags(t *testing.T) {
	err := runApplyFlags([]string{"-f", "docker-compose.yml"})
	if err == nil || !strings.Contains(err.Error(), "--name") {
		t.Errorf("expected error about --name, got %v", err)
	}

	err = runApplyFlags([]string{"-n", "mystack"})
	if err == nil || !strings.Contains(err.Error(), "--compose-file") {
		t.Errorf("expected error about --compose-file, got %v", err)
	}
}

func runApplyFlags(args []string) error {
	_, err := parseApplyArgs(args)
	return err
}
