package swarm

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
)

type testNetInterface struct {
	addrs []net.Addr
	err   error
}

func (t testNetInterface) Addrs() ([]net.Addr, error) {
	return t.addrs, t.err
}

type testAddr struct {
	value string
}

func (t testAddr) Network() string {
	return "test"
}

func (t testAddr) String() string {
	return t.value
}

func TestDetectHostGatewayIP_NodeListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("node list failed")
	mock := &MockDockerClient{
		NodeListFn: func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) {
			return nil, wantErr
		},
	}

	_, err := detectHostGatewayIP(context.Background(), mock, func(string) (netInterface, error) {
		t.Fatal("lookup should not be called on node list error")
		return nil, nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped node list error, got %v", err)
	}
}

func TestDetectHostGatewayIP_InterfaceLookupError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("docker0 not found")
	mock := &MockDockerClient{
		NodeListFn: func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "node1"}}, nil
		},
	}

	_, err := detectHostGatewayIP(context.Background(), mock, func(string) (netInterface, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped lookup error, got %v", err)
	}
}

func TestDetectHostGatewayIP_InterfaceAddressesError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("address lookup failed")
	mock := &MockDockerClient{
		NodeListFn: func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "node1"}}, nil
		},
	}

	_, err := detectHostGatewayIP(context.Background(), mock, func(string) (netInterface, error) {
		return testNetInterface{err: wantErr}, nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped addresses error, got %v", err)
	}
}

func TestDetectHostGatewayIP_NoAddresses(t *testing.T) {
	t.Parallel()

	mock := &MockDockerClient{
		NodeListFn: func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "node1"}}, nil
		},
	}

	_, err := detectHostGatewayIP(context.Background(), mock, func(string) (netInterface, error) {
		return testNetInterface{addrs: nil}, nil
	})
	if err == nil || err.Error() != "docker0 has no addresses" {
		t.Fatalf("expected docker0 no addresses error, got %v", err)
	}
}

func TestDetectHostGatewayIP_NoCIDRAddress(t *testing.T) {
	t.Parallel()

	mock := &MockDockerClient{
		NodeListFn: func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "node1"}}, nil
		},
	}

	_, err := detectHostGatewayIP(context.Background(), mock, func(string) (netInterface, error) {
		return testNetInterface{
			addrs: []net.Addr{
				testAddr{value: "invalid-address"},
				testAddr{value: "still-invalid"},
			},
		}, nil
	})
	if err == nil || err.Error() != "docker0 has no CIDR address" {
		t.Fatalf("expected docker0 no CIDR address error, got %v", err)
	}
}

func TestDetectHostGatewayIP_Success(t *testing.T) {
	t.Parallel()

	mock := &MockDockerClient{
		NodeListFn: func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "node1"}}, nil
		},
	}

	got, err := detectHostGatewayIP(context.Background(), mock, func(string) (netInterface, error) {
		return testNetInterface{
			addrs: []net.Addr{
				testAddr{value: "172.18.0.1/16"},
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "172.18.0.1" {
		t.Fatalf("expected 172.18.0.1, got %s", got)
	}
}

func TestFirstCIDRIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		addrs   []net.Addr
		wantIP  string
		wantErr string
	}{
		{
			name: "returns first valid CIDR address",
			addrs: []net.Addr{
				testAddr{value: "invalid"},
				testAddr{value: "10.0.0.2/24"},
				testAddr{value: "10.0.0.3/24"},
			},
			wantIP: "10.0.0.2",
		},
		{
			name: "returns error when no CIDR address exists",
			addrs: []net.Addr{
				testAddr{value: "invalid"},
			},
			wantErr: "docker0 has no CIDR address",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := firstCIDRIP(tc.addrs)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("expected error %q, got %v", tc.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantIP {
				t.Fatalf("expected IP %s, got %s", tc.wantIP, got)
			}
		})
	}
}
