package paths

import (
	"os"
	"path/filepath"
	"strings"
)

var specialProtocols = []string{"nfs://", "cifs://", "tmpfs"}

type Resolver struct {
	basePath string
}

func NewResolver() (*Resolver, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	return &Resolver{basePath: cwd}, nil
}

func (r *Resolver) BasePath() string {
	return r.basePath
}

// ResolverWithBase is an exported resolver variant for tests.
type ResolverWithBase struct {
	Base string
}

func (r *ResolverWithBase) Resolve(source string) string {
	return resolve(source, r.Base)
}

func (r *Resolver) Resolve(source string) string {
	return resolve(source, r.basePath)
}

func resolve(source, base string) string {
	if filepath.IsAbs(source) {
		return source
	}

	if isSpecialProtocol(source) {
		return source
	}

	expanded := expandTilde(source)
	if filepath.IsAbs(expanded) {
		return expanded
	}

	return filepath.Clean(filepath.Join(base, expanded))
}

func expandTilde(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}

	return path
}

func isSpecialProtocol(path string) bool {
	for _, p := range specialProtocols {
		if strings.HasPrefix(path, p) || path == p {
			return true
		}
	}

	return false
}
