package swarm

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/filters"

	"github.com/SomeBlackMagic/stackman/internal/compose"
)

// EnsureVolumes creates missing stack-scoped volumes from compose config.
// The operation is idempotent: existing volumes are skipped.
func EnsureVolumes(ctx context.Context, client DockerClient, stackName string, volumes map[string]*compose.Volume) error {
	existing, err := client.VolumeList(ctx, filters.NewArgs(filters.Arg("label", stackNamespaceLabel+"="+stackName)))
	if err != nil {
		return fmt.Errorf("list volumes: %w", err)
	}

	existingByName := make(map[string]struct{}, len(existing.Volumes))
	for _, vol := range existing.Volumes {
		existingByName[vol.Name] = struct{}{}
	}

	prefix := stackName + "_"
	for name, cfg := range volumes {
		if cfg == nil {
			return fmt.Errorf("convert volume %q: nil volume config", name)
		}
		if isExternalVolume(cfg) {
			continue
		}

		fullName := prefix + name
		if _, ok := existingByName[fullName]; ok {
			continue
		}

		opts, err := compose.ConvertVolume(name, *cfg, stackName)
		if err != nil {
			return fmt.Errorf("convert volume %q: %w", name, err)
		}
		opts.Name = fullName

		if _, err := client.VolumeCreate(ctx, opts); err != nil {
			return fmt.Errorf("create volume %q: %w", fullName, err)
		}
	}

	return nil
}

func isExternalVolume(cfg *compose.Volume) bool {
	if cfg.External == nil {
		return false
	}

	switch value := cfg.External.(type) {
	case bool:
		return value
	case map[string]interface{}:
		return true
	default:
		return false
	}
}
