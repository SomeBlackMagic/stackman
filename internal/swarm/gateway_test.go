package swarm

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/network"
	dockerswarm "github.com/docker/docker/api/types/swarm"
)

// gatewayMockClient implements only the subset of DockerClient needed for gateway tests.
type gatewayMockClient struct {
	networkInspectFn func(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
	nodeListFn       func(ctx context.Context, options dockerswarm.NodeListOptions) ([]dockerswarm.Node, error)
}

func (m *gatewayMockClient) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	if m.networkInspectFn != nil {
		return m.networkInspectFn(ctx, networkID, options)
	}
	return network.Inspect{}, nil
}

func (m *gatewayMockClient) NodeList(ctx context.Context, options dockerswarm.NodeListOptions) ([]dockerswarm.Node, error) {
	if m.nodeListFn != nil {
		return m.nodeListFn(ctx, options)
	}
	return nil, nil
}

func TestDetectHostGatewayIP(t *testing.T) {
	tests := []struct {
		name        string
		mockFn      func(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
		expectedIP  string
		expectError bool
	}{
		{
			name: "returns gateway from bridge IPAM config",
			mockFn: func(_ context.Context, networkID string, _ network.InspectOptions) (network.Inspect, error) {
				return network.Inspect{
					IPAM: network.IPAM{
						Config: []network.IPAMConfig{
							{Subnet: "172.17.0.0/16", Gateway: "172.17.0.1"},
						},
					},
				}, nil
			},
			expectedIP: "172.17.0.1",
		},
		{
			name: "returns first non-empty gateway from multiple IPAM configs",
			mockFn: func(_ context.Context, _ string, _ network.InspectOptions) (network.Inspect, error) {
				return network.Inspect{
					IPAM: network.IPAM{
						Config: []network.IPAMConfig{
							{Subnet: "172.17.0.0/16", Gateway: ""},
							{Subnet: "10.0.0.0/8", Gateway: "10.0.0.1"},
						},
					},
				}, nil
			},
			expectedIP: "10.0.0.1",
		},
		{
			name: "error when NetworkInspect fails",
			mockFn: func(_ context.Context, _ string, _ network.InspectOptions) (network.Inspect, error) {
				return network.Inspect{}, errors.New("network not found")
			},
			expectError: true,
		},
		{
			name: "error when IPAM config is empty",
			mockFn: func(_ context.Context, _ string, _ network.InspectOptions) (network.Inspect, error) {
				return network.Inspect{
					IPAM: network.IPAM{Config: []network.IPAMConfig{}},
				}, nil
			},
			expectError: true,
		},
		{
			name: "error when all IPAM gateways are empty",
			mockFn: func(_ context.Context, _ string, _ network.InspectOptions) (network.Inspect, error) {
				return network.Inspect{
					IPAM: network.IPAM{
						Config: []network.IPAMConfig{
							{Subnet: "172.17.0.0/16", Gateway: ""},
						},
					},
				}, nil
			},
			expectError: true,
		},
		{
			name: "inspects bridge network specifically",
			mockFn: func(_ context.Context, networkID string, _ network.InspectOptions) (network.Inspect, error) {
				if networkID != "bridge" {
					return network.Inspect{}, errors.New("unexpected network ID: " + networkID)
				}
				return network.Inspect{
					IPAM: network.IPAM{
						Config: []network.IPAMConfig{{Gateway: "172.17.0.1"}},
					},
				}, nil
			},
			expectedIP: "172.17.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &gatewayMockClient{networkInspectFn: tt.mockFn}
			ip, err := DetectHostGatewayIP(context.Background(), mock)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ip != tt.expectedIP {
				t.Errorf("expected IP %q, got %q", tt.expectedIP, ip)
			}
		})
	}
}

func TestIsMultiNodeSwarm(t *testing.T) {
	tests := []struct {
		name        string
		mockFn      func(ctx context.Context, options dockerswarm.NodeListOptions) ([]dockerswarm.Node, error)
		expected    bool
		expectError bool
	}{
		{
			name: "single node returns false",
			mockFn: func(_ context.Context, _ dockerswarm.NodeListOptions) ([]dockerswarm.Node, error) {
				return []dockerswarm.Node{{ID: "node1"}}, nil
			},
			expected: false,
		},
		{
			name: "three nodes returns true",
			mockFn: func(_ context.Context, _ dockerswarm.NodeListOptions) ([]dockerswarm.Node, error) {
				return []dockerswarm.Node{
					{ID: "node1"},
					{ID: "node2"},
					{ID: "node3"},
				}, nil
			},
			expected: true,
		},
		{
			name: "two nodes returns true",
			mockFn: func(_ context.Context, _ dockerswarm.NodeListOptions) ([]dockerswarm.Node, error) {
				return []dockerswarm.Node{{ID: "node1"}, {ID: "node2"}}, nil
			},
			expected: true,
		},
		{
			name: "NodeList error returns error",
			mockFn: func(_ context.Context, _ dockerswarm.NodeListOptions) ([]dockerswarm.Node, error) {
				return nil, errors.New("swarm not initialized")
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &gatewayMockClient{nodeListFn: tt.mockFn}
			result, err := IsMultiNodeSwarm(context.Background(), mock)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
