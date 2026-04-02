package swarm_test

import (
	"context"
	"errors"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"

	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestIsMultiNodeSwarm_SingleNode(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "node1"}}, nil
		},
	}

	multi, err := swarmint.IsMultiNodeSwarm(context.Background(), mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if multi {
		t.Error("single node should not be multi-node")
	}
}

func TestIsMultiNodeSwarm_MultiNode(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "node1"}, {ID: "node2"}}, nil
		},
	}

	multi, err := swarmint.IsMultiNodeSwarm(context.Background(), mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !multi {
		t.Error("two nodes should be multi-node")
	}
}

func TestDetectHostGatewayIP_MultiNodeError(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "node1"}, {ID: "node2"}}, nil
		},
	}

	_, err := swarmint.DetectHostGatewayIP(context.Background(), mock)
	if !errors.Is(err, swarmint.ErrMultiNodeSwarm) {
		t.Fatalf("expected ErrMultiNodeSwarm, got %v", err)
	}
}

func TestIsMultiNodeSwarm_NodeListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("node list failed")
	mock := &swarmint.MockDockerClient{
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return nil, wantErr
		},
	}

	_, err := swarmint.IsMultiNodeSwarm(context.Background(), mock)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped node list error, got %v", err)
	}
}
