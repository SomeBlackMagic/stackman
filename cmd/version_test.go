package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	origVersion, origCommit, origDate := Version, Commit, BuildDate
	Version, Commit, BuildDate = "1.0.0", "abc1234", "2026-01-01T00:00:00Z"
	defer func() {
		Version, Commit, BuildDate = origVersion, origCommit, origDate
	}()

	buf := &bytes.Buffer{}
	ExecuteVersionTo(buf)
	out := buf.String()

	for _, want := range []string{"1.0.0", "abc1234", "2026-01-01T00:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("version output missing %q, got:\n%s", want, out)
		}
	}
}
