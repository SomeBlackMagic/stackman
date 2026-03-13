package swarm

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/network"
	dockerswarm "github.com/docker/docker/api/types/swarm"
)

// networkInspector is a minimal interface for Docker network inspection.
type networkInspector interface {
	NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
}

// nodeCounter is a minimal interface for listing Swarm nodes.
type nodeCounter interface {
	NodeList(ctx context.Context, options dockerswarm.NodeListOptions) ([]dockerswarm.Node, error)
}

// DetectHostGatewayIP returns the Docker host gateway IP by inspecting
// the IPAM config of the "bridge" (docker0) network.
// This is the same value Docker Compose resolves for the "host-gateway" special token.
func DetectHostGatewayIP(ctx context.Context, cli networkInspector) (string, error) {
	inspect, err := cli.NetworkInspect(ctx, "bridge", network.InspectOptions{})
	if err != nil {
		return "", fmt.Errorf("cannot resolve host-gateway: failed to inspect bridge network: %w", err)
	}
	for _, cfg := range inspect.IPAM.Config {
		if cfg.Gateway != "" {
			return cfg.Gateway, nil
		}
	}
	return "", fmt.Errorf("cannot resolve host-gateway: no gateway found in bridge network IPAM config")
}

// IsMultiNodeSwarm returns true if the Swarm cluster contains more than one node.
func IsMultiNodeSwarm(ctx context.Context, cli nodeCounter) (bool, error) {
	nodes, err := cli.NodeList(ctx, dockerswarm.NodeListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list swarm nodes: %w", err)
	}
	return len(nodes) > 1, nil
}

// hasHostGateway returns true if at least one entry in hosts contains the "host-gateway" token.
func hasHostGateway(hosts []string) bool {
	for _, h := range hosts {
		if strings.Contains(h, "host-gateway") {
			return true
		}
	}
	return false
}
