package swarm

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/network"
	dockerswarm "github.com/docker/docker/api/types/swarm"
)

// networkInspector — минимальный интерфейс для инспекции сетей Docker.
type networkInspector interface {
	NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
}

// nodeCounter — минимальный интерфейс для получения списка узлов Swarm.
type nodeCounter interface {
	NodeList(ctx context.Context, options dockerswarm.NodeListOptions) ([]dockerswarm.Node, error)
}

// DetectHostGatewayIP возвращает IP-адрес шлюза Docker-хоста,
// инспектируя IPAM-конфиг сети "bridge" (docker0).
// Это то же значение, которое Docker Compose использует для "host-gateway".
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

// IsMultiNodeSwarm возвращает true если кластер Swarm содержит более одного узла.
func IsMultiNodeSwarm(ctx context.Context, cli nodeCounter) (bool, error) {
	nodes, err := cli.NodeList(ctx, dockerswarm.NodeListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list swarm nodes: %w", err)
	}
	return len(nodes) > 1, nil
}

// hasHostGateway возвращает true если хотя бы одна запись в hosts содержит "host-gateway".
func hasHostGateway(hosts []string) bool {
	for _, h := range hosts {
		if strings.Contains(h, "host-gateway") {
			return true
		}
	}
	return false
}
