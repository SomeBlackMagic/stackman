package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// Resolver handles path resolution for Docker volumes and bind mounts
type Resolver struct {
	basePath string
}

// NewResolver creates a new path resolver
// It uses SWARM_STACK_PATH environment variable or current working directory
func NewResolver() (*Resolver, error) {
	basePath := os.Getenv("SWARM_STACK_PATH")
	if basePath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		basePath = cwd
	}

	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, err
	}

	return &Resolver{
		basePath: absPath,
	}, nil
}

// Resolve converts relative paths to absolute paths
// Based on the algorithm from the tech spec:
// - ./path and ../path are resolved relative to basePath
// - Relative paths without ./ or ../ are resolved relative to basePath
// - Absolute paths are returned as-is
// - Paths with :// (like nfs://) are returned as-is
func (r *Resolver) Resolve(source string) string {
	// Already absolute path
	if strings.HasPrefix(source, "/") {
		return source
	}

	// Special protocols (nfs://, cifs://, etc.)
	if strings.Contains(source, "://") {
		return source
	}

	// ./path or ../path - resolve relative to basePath
	if strings.HasPrefix(source, "./") {
		return filepath.Join(r.basePath, strings.TrimPrefix(source, "./"))
	}

	if strings.HasPrefix(source, "../") {
		return filepath.Join(r.basePath, source)
	}

	// Regular relative path - resolve relative to basePath
	return filepath.Join(r.basePath, source)
}

// BasePath returns the base path used for resolution
func (r *Resolver) BasePath() string {
	return r.basePath
}
