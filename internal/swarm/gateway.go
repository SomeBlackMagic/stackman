package swarm

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/docker/docker/api/types"
)

// ErrMultiNodeSwarm indicates host-gateway detection is unsupported in multi-node swarm.
var ErrMultiNodeSwarm = errors.New("host-gateway resolution is not supported in multi-node swarm")

type netInterface interface {
	Addrs() ([]net.Addr, error)
}

// IsMultiNodeSwarm reports whether swarm has more than one node.
func IsMultiNodeSwarm(ctx context.Context, client DockerClient) (bool, error) {
	nodes, err := client.NodeList(ctx, types.NodeListOptions{})
	if err != nil {
		return false, fmt.Errorf("list nodes: %w", err)
	}
	return len(nodes) > 1, nil
}

// DetectHostGatewayIP detects docker0 bridge IP on single-node swarm.
func DetectHostGatewayIP(ctx context.Context, client DockerClient) (string, error) {
	return detectHostGatewayIP(ctx, client, func(name string) (netInterface, error) {
		return net.InterfaceByName(name)
	})
}

func detectHostGatewayIP(ctx context.Context, client DockerClient, lookupInterface func(name string) (netInterface, error)) (string, error) {
	multiNode, err := IsMultiNodeSwarm(ctx, client)
	if err != nil {
		return "", fmt.Errorf("detect swarm topology: %w", err)
	}
	if multiNode {
		return "", ErrMultiNodeSwarm
	}

	iface, err := lookupInterface("docker0")
	if err != nil {
		return "", fmt.Errorf("find docker0 interface: %w", err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", fmt.Errorf("list docker0 addresses: %w", err)
	}
	if len(addrs) == 0 {
		return "", errors.New("docker0 has no addresses")
	}

	ip, err := firstCIDRIP(addrs)
	if err != nil {
		return "", err
	}
	return ip, nil
}

func firstCIDRIP(addrs []net.Addr) (string, error) {
	for _, addr := range addrs {
		ip, _, parseErr := net.ParseCIDR(addr.String())
		if parseErr == nil && ip != nil {
			return ip.String(), nil
		}
	}
	return "", errors.New("docker0 has no CIDR address")
}
