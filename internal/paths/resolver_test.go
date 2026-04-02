package paths_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SomeBlackMagic/stackman/internal/paths"
)

func TestResolve_AbsolutePath(t *testing.T) {
	r, err := paths.NewResolver()
	if err != nil {
		t.Fatal(err)
	}

	got := r.Resolve("/etc/docker/compose.yml")
	if got != "/etc/docker/compose.yml" {
		t.Errorf("absolute path should be unchanged, got %q", got)
	}
}

func TestNewResolver_BasePathMatchesCWD(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	r, err := paths.NewResolver()
	if err != nil {
		t.Fatal(err)
	}

	if r.BasePath() != cwd {
		t.Errorf("got base path %q, want %q", r.BasePath(), cwd)
	}
}

func TestNewResolver_GetwdError(t *testing.T) {
	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalCWD); chdirErr != nil {
			t.Fatalf("failed to restore cwd: %v", chdirErr)
		}
	})

	if err := os.RemoveAll(tmpDir); err != nil {
		t.Fatal(err)
	}

	if _, err := paths.NewResolver(); err == nil {
		t.Fatal("expected NewResolver to fail when cwd is removed")
	}
}

func TestResolve_RelativePath(t *testing.T) {
	r := &paths.ResolverWithBase{Base: "/project"}

	got := r.Resolve("./stacks/app.yml")
	want := "/project/stacks/app.yml"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolve_RelativeNoPrefix(t *testing.T) {
	r := &paths.ResolverWithBase{Base: "/project"}

	got := r.Resolve("app.yml")
	want := "/project/app.yml"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolve_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	r := &paths.ResolverWithBase{Base: "/project"}

	got := r.Resolve("~/stacks/app.yml")
	want := filepath.Join(home, "stacks", "app.yml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolve_TildeOnly(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	r := &paths.ResolverWithBase{Base: "/project"}

	got := r.Resolve("~")
	if got != home {
		t.Errorf("got %q, want %q", got, home)
	}
}

func TestResolve_SpecialProtocols(t *testing.T) {
	r := &paths.ResolverWithBase{Base: "/project"}

	cases := []string{
		"nfs://host/path",
		"cifs://host/share",
		"tmpfs",
	}

	for _, c := range cases {
		got := r.Resolve(c)
		if got != c {
			t.Errorf("special path %q should be unchanged, got %q", c, got)
		}
	}
}
