package snapshot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	dockernetwork "github.com/docker/docker/api/types/network"
	dockerswarm "github.com/docker/docker/api/types/swarm"
	dockervolume "github.com/docker/docker/api/types/volume"

	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

// StackSnapshot stores stack resources captured before deployment.
type StackSnapshot struct {
	StackName     string
	TakenAt       time.Time
	IsFirstDeploy bool

	// Keys are normalized names without "<stack>_" prefix.
	Services map[string]dockerswarm.Service
	Networks map[string]dockernetwork.Summary
	Volumes  map[string]dockervolume.Volume
}

// TakeSnapshot captures current stack state before deployment.
func TakeSnapshot(ctx context.Context, deployer *swarmint.StackDeployer) (*StackSnapshot, error) {
	if deployer == nil {
		return nil, fmt.Errorf("take snapshot: nil deployer")
	}

	state, err := deployer.GetState(ctx)
	if err != nil {
		return nil, fmt.Errorf("take snapshot: get stack state: %w", err)
	}

	snapshot := &StackSnapshot{
		StackName:     deployer.StackName(),
		TakenAt:       time.Now().UTC(),
		IsFirstDeploy: len(state.Services) == 0,
		Services:      cloneServices(state.Services),
		Networks:      cloneNetworks(state.Networks),
		Volumes:       cloneVolumes(state.Volumes),
	}

	return snapshot, nil
}

// Rollback restores stack resources to the snapshot state.
func (s *StackSnapshot) Rollback(ctx context.Context, client swarmint.DockerClient) error {
	if s == nil {
		return fmt.Errorf("rollback snapshot: nil snapshot")
	}
	if s.StackName == "" {
		return fmt.Errorf("rollback snapshot: empty stack name")
	}

	current, err := swarmint.GetStackState(ctx, client, s.StackName)
	if err != nil {
		return fmt.Errorf("rollback snapshot: get current state: %w", err)
	}

	if s.IsFirstDeploy {
		if err := s.rollbackFirstDeploy(ctx, client, current); err != nil {
			return fmt.Errorf("rollback snapshot: first deploy: %w", err)
		}
		return nil
	}

	if err := s.rollbackServices(ctx, client, current); err != nil {
		return fmt.Errorf("rollback snapshot: services: %w", err)
	}
	if err := s.rollbackNetworks(ctx, client, current); err != nil {
		return fmt.Errorf("rollback snapshot: networks: %w", err)
	}
	if err := s.rollbackVolumes(ctx, client, current); err != nil {
		return fmt.Errorf("rollback snapshot: volumes: %w", err)
	}

	return nil
}

func (s *StackSnapshot) rollbackFirstDeploy(ctx context.Context, client swarmint.DockerClient, current *swarmint.StackState) error {
	for name, service := range current.Services {
		if err := client.ServiceRemove(ctx, service.ID); err != nil {
			return fmt.Errorf("remove service %q: %w", name, err)
		}
	}

	for name, net := range current.Networks {
		netID := resourceID(net.ID, net.Name)
		if err := client.NetworkRemove(ctx, netID); err != nil {
			return fmt.Errorf("remove network %q: %w", name, err)
		}
	}

	for name, vol := range current.Volumes {
		volID := resourceID(vol.Name, name)
		if err := client.VolumeRemove(ctx, volID, true); err != nil {
			return fmt.Errorf("remove volume %q: %w", name, err)
		}
	}

	return nil
}

func (s *StackSnapshot) rollbackServices(ctx context.Context, client swarmint.DockerClient, current *swarmint.StackState) error {
	for name, service := range current.Services {
		if _, inSnapshot := s.Services[name]; inSnapshot {
			continue
		}
		if err := client.ServiceRemove(ctx, service.ID); err != nil {
			return fmt.Errorf("remove new service %q: %w", name, err)
		}
	}

	for name, snapshotSvc := range s.Services {
		slog.InfoContext(ctx, "rollback service", "stack", s.StackName, "service", name)

		currentSvc, exists := current.Services[name]
		if !exists {
			if _, err := client.ServiceCreate(ctx, snapshotSvc.Spec, dockertypes.ServiceCreateOptions{}); err != nil {
				return fmt.Errorf("recreate service %q: %w", name, err)
			}
			continue
		}

		if _, err := client.ServiceUpdate(ctx, currentSvc.ID, currentSvc.Version, snapshotSvc.Spec, dockertypes.ServiceUpdateOptions{}); err != nil {
			return fmt.Errorf("restore service %q: %w", name, err)
		}
	}

	return nil
}

func (s *StackSnapshot) rollbackNetworks(ctx context.Context, client swarmint.DockerClient, current *swarmint.StackState) error {
	for name, net := range current.Networks {
		if _, inSnapshot := s.Networks[name]; inSnapshot {
			continue
		}
		netID := resourceID(net.ID, net.Name)
		if err := client.NetworkRemove(ctx, netID); err != nil {
			return fmt.Errorf("remove new network %q: %w", name, err)
		}
	}

	for name, snapshotNet := range s.Networks {
		if _, exists := current.Networks[name]; exists {
			continue
		}

		createOpts := dockernetwork.CreateOptions{
			Driver:     snapshotNet.Driver,
			Scope:      snapshotNet.Scope,
			EnableIPv4: boolPtr(snapshotNet.EnableIPv4),
			EnableIPv6: boolPtr(snapshotNet.EnableIPv6),
			IPAM:       &snapshotNet.IPAM,
			Internal:   snapshotNet.Internal,
			Attachable: snapshotNet.Attachable,
			Ingress:    snapshotNet.Ingress,
			ConfigOnly: snapshotNet.ConfigOnly,
			ConfigFrom: &snapshotNet.ConfigFrom,
			Options:    cloneStringMap(snapshotNet.Options),
			Labels:     cloneStringMap(snapshotNet.Labels),
		}

		if snapshotNet.IPAM.Driver == "" && len(snapshotNet.IPAM.Config) == 0 && len(snapshotNet.IPAM.Options) == 0 {
			createOpts.IPAM = nil
		}
		if snapshotNet.ConfigFrom.Network == "" {
			createOpts.ConfigFrom = nil
		}

		if _, err := client.NetworkCreate(ctx, snapshotNet.Name, createOpts); err != nil {
			return fmt.Errorf("recreate network %q: %w", name, err)
		}
	}

	return nil
}

func (s *StackSnapshot) rollbackVolumes(ctx context.Context, client swarmint.DockerClient, current *swarmint.StackState) error {
	for name, vol := range current.Volumes {
		if _, inSnapshot := s.Volumes[name]; inSnapshot {
			continue
		}
		volID := resourceID(vol.Name, name)
		if err := client.VolumeRemove(ctx, volID, true); err != nil {
			return fmt.Errorf("remove new volume %q: %w", name, err)
		}
	}

	for name, snapshotVol := range s.Volumes {
		if _, exists := current.Volumes[name]; exists {
			continue
		}

		createOpts := dockervolume.CreateOptions{
			Name:       snapshotVol.Name,
			Driver:     snapshotVol.Driver,
			DriverOpts: cloneStringMap(snapshotVol.Options),
			Labels:     cloneStringMap(snapshotVol.Labels),
		}
		if _, err := client.VolumeCreate(ctx, createOpts); err != nil {
			return fmt.Errorf("recreate volume %q: %w", name, err)
		}
	}

	return nil
}

func resourceID(primary string, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func boolPtr(v bool) *bool {
	value := v
	return &value
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func cloneServices(src map[string]dockerswarm.Service) map[string]dockerswarm.Service {
	if len(src) == 0 {
		return map[string]dockerswarm.Service{}
	}
	dst := make(map[string]dockerswarm.Service, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func cloneNetworks(src map[string]dockernetwork.Summary) map[string]dockernetwork.Summary {
	if len(src) == 0 {
		return map[string]dockernetwork.Summary{}
	}
	dst := make(map[string]dockernetwork.Summary, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func cloneVolumes(src map[string]dockervolume.Volume) map[string]dockervolume.Volume {
	if len(src) == 0 {
		return map[string]dockervolume.Volume{}
	}
	dst := make(map[string]dockervolume.Volume, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
