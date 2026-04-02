package swarm

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"

	"github.com/SomeBlackMagic/stackman/internal/compose"
)

// EnsureNetworks creates missing stack-scoped networks from compose config.
// The operation is idempotent: existing networks are skipped.
func EnsureNetworks(ctx context.Context, client DockerClient, stackName string, networks map[string]*compose.Network) error {
	existing, err := client.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", stackNamespaceLabel+"="+stackName)),
	})
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}

	existingByName := make(map[string]struct{}, len(existing))
	for _, net := range existing {
		existingByName[net.Name] = struct{}{}
	}

	prefix := stackName + "_"
	for name, cfg := range networks {
		if cfg == nil {
			return fmt.Errorf("convert network %q: nil network config", name)
		}
		if isExternalNetwork(cfg) {
			continue
		}

		fullName := prefix + name
		if _, ok := existingByName[fullName]; ok {
			continue
		}

		opts, err := compose.ConvertNetwork(name, *cfg, stackName)
		if err != nil {
			return fmt.Errorf("convert network %q: %w", name, err)
		}
		if _, err := client.NetworkCreate(ctx, fullName, opts); err != nil {
			return fmt.Errorf("create network %q: %w", fullName, err)
		}
	}

	return nil
}

func isExternalNetwork(cfg *compose.Network) bool {
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
