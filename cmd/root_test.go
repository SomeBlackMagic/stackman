package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelp(t *testing.T) {
	buf := &bytes.Buffer{}
	command := NewRootCommand()
	command.SetOut(buf)
	command.SetErr(buf)
	command.SetArgs([]string{"--help"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	out := buf.String()
	for _, want := range []string{"apply", "rollback", "logs", "events", "diff", "status", "version"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help output missing command %q", want)
		}
	}
}

func TestStubCommandsReturnNotImplemented(t *testing.T) {
	stubs := []string{"diff", "status"}

	for _, stub := range stubs {
		t.Run(stub, func(t *testing.T) {
			command := NewRootCommand()
			command.SetArgs([]string{stub})

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected error for %q command", stub)
			}
			if !strings.Contains(err.Error(), "not implemented yet") {
				t.Fatalf("expected not implemented error, got %v", err)
			}
		})
	}
}
